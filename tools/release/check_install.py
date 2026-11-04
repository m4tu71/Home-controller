#!/usr/bin/env python3
"""Validate install examples using temporary fixtures; never change the host config."""
import argparse
import json
import os
from pathlib import Path
import re
import shutil
import socket
import subprocess
from urllib.parse import unquote

REPO = Path(__file__).resolve().parents[2]


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--output", required=True, type=Path)
    args = p.parse_args()
    out = args.output.resolve()
    if out.exists():
        raise ValueError("choose a new output directory; existing evidence is preserved")
    out.mkdir(parents=True)
    checks = []

    def run(name, command, cwd=REPO):
        print(name, flush=True)
        with (out / (name + ".log")).open("w") as log:
            result = subprocess.run(command, cwd=cwd, stdout=log, stderr=subprocess.STDOUT,
                                    env=dict(os.environ, GOCACHE=str(REPO / ".artifacts/go-cache")))
        checks.append({"name": name, "command": list(map(str, command)), "exit_code": result.returncode})
        if result.returncode:
            raise RuntimeError(f"{name} failed: see {out / (name + '.log')}")

    report = {"checks": checks, "host_modified": False, "service_started": False}
    try:
        run("config-and-template-contracts", ["go", "test", "./..."], REPO / "config")
        root = out / "systemd-root"
        units = root / "etc/systemd/system"
        units.mkdir(parents=True)
        for name in ["web-controller-backend.service", "web-controller-daemon.service"]:
            shutil.copy2(REPO / "examples/systemd" / name, units / name)
        for name in ["sysinit", "basic", "network", "local-fs", "multi-user", "shutdown", "sockets", "paths", "timers"]:
            (units / (name + ".target")).write_text("[Unit]\nDescription=Offline validation fixture\nDefaultDependencies=no\n")
        bins = root / "opt/web-controller/current/bin"
        bins.mkdir(parents=True)
        for name in ["backend", "controller-daemon"]:
            shutil.copy2("/bin/true", bins / name)  # Existence/executable fixture; never executed.
        (root / "etc/passwd").write_text("root:x:0:0::/root:/bin/sh\nweb-controller:x:61111:61111::/var/lib/web-controller:/usr/sbin/nologin\n")
        (root / "etc/group").write_text("root:x:0:\nweb-controller:x:61111:\ndialout:x:20:\n")
        run("systemd-offline-verify", ["systemd-analyze", "--root=" + str(root), "--generators=no", "--man=no", "verify", "web-controller-backend.service", "web-controller-daemon.service"])
        nginx = out / "nginx"
        nginx.mkdir()
        # nginx -t checks sockets too: use loopback/unprivileged ports only in the fixture.
        with socket.socket() as probe:
            probe.bind(("127.0.0.1", 0))
            port = probe.getsockname()[1]
        server = nginx / "web-controller.conf"
        server.write_text((REPO / "examples/nginx/web-controller.conf").read_text()
                          .replace("listen 80;", f"listen 127.0.0.1:{port};")
                          .replace("listen [::]:80;", f"listen [::1]:{port};"))
        config = nginx / "nginx.conf"
        temps = "".join(f' {name}_temp_path "{nginx}/{name}";\n' for name in ["client_body", "proxy", "fastcgi", "uwsgi", "scgi"])
        config.write_text(f'error_log stderr;\npid "{nginx}/nginx.pid";\nevents {{}}\nhttp {{\n access_log off;\n{temps} include "{server}";\n}}\n')
        report["nginx_fixture"] = "Only listen addresses/ports changed to loopback ephemeral port; no daemon launched"
        run("nginx-syntax", ["nginx", "-t", "-e", "stderr", "-p", str(nginx) + "/", "-c", str(config)])
        checked = []
        for name in ["README.md", "INSTALL.md", "examples/README.md", "docs/OPERATIONS.md", "docs/API.md", "docs/DEVELOPMENT.md", "docs/TROUBLESHOOTING.md"]:
            doc = REPO / name
            text = doc.read_text()
            for target in re.findall(r"\[[^\]]+\]\(([^)]+)\)", text):
                if "://" in target or target.startswith(("#", "mailto:")):
                    continue
                target = unquote(target.split("#", 1)[0])
                if not (doc.parent / target).exists():
                    raise ValueError(f"broken local link: {name} -> {target}")
                checked.append([name, target])
        for name in ["README.md", "INSTALL.md", "docs/API.md", "docs/OPERATIONS.md"]:
            text = (REPO / name).read_text()
            if re.search(r"\bbackend\s+maintenance\s", text):
                raise ValueError(f"CLI example missing --config: {name}")
        report["local_links_checked"] = checked
        report["passed"] = True
        print("Install template validation passed", flush=True)
    except Exception as error:
        report["passed"] = False
        report["error"] = str(error)
        raise
    finally:
        (out / "report.json").write_text(json.dumps(report, indent=2) + "\n")


if __name__ == "__main__":
    main()
