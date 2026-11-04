#!/usr/bin/env python3
"""Build and verify release binaries, web assets and firmware; never deploy, flash or publish."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import struct
import subprocess
import sys

REPO = Path(__file__).resolve().parents[2]
PREFIXES = {"backend", "controller-daemon", "config", "frontend", "pico-firmware", "examples"}


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, required=True, help="new, non-existing directory")
    parser.add_argument("--require-clean", action="store_true", help="reject uncommitted source; use for final candidates")
    parser.add_argument("--install-deps", action="store_true", help="npm ci in the isolated tree instead of reusing installed dependencies")
    args = parser.parse_args()
    version = (REPO / "VERSION").read_text().strip()
    if not re.fullmatch(r"\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?", version):
        raise ValueError("invalid VERSION")
    pkg = json.loads((REPO / "frontend/package.json").read_text())
    lock = json.loads((REPO / "frontend/package-lock.json").read_text())
    if {pkg["version"], lock["version"], lock["packages"][""]["version"]} != {version}:
        raise ValueError("VERSION/package/lock versions must match")
    status = subprocess.check_output(["git", "status", "--porcelain"], cwd=REPO, text=True)
    if args.require_clean and status:
        raise ValueError("clean Git source required; commit reviewed changes manually before building")
    output = args.output.resolve()
    if output.exists():
        raise ValueError("output already exists; choose a new directory (nothing is deleted)")
    # Output must not become an input to the snapshot.
    if output == REPO or any(output.is_relative_to(REPO / p) for p in PREFIXES):
        raise ValueError("output overlaps a source directory")
    output.mkdir(parents=True)
    logs = output / "logs"
    logs.mkdir()
    env = dict(os.environ, GOCACHE=str(REPO / ".artifacts/go-cache"))
    report = {"version": version, "git_head": subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=REPO, text=True).strip(),
              "dirty": bool(status), "source_status": status, "dependency_mode": "npm-ci" if args.install_deps else "installed-node-modules",
              "verification": [], "artifacts": {}, "release_ready": False}

    def run(name, cmd, cwd=None, extra=None):
        print(f"[{name}] {' '.join(map(str, cmd))}", flush=True)
        with (logs / (name + ".log")).open("w") as log:
            result = subprocess.run(list(map(str, cmd)), cwd=cwd or REPO, env=env | (extra or {}), stdout=log, stderr=subprocess.STDOUT)
        report["verification"].append({"name": name, "command": list(map(str, cmd)), "exit_code": result.returncode})
        if result.returncode:
            raise RuntimeError(f"{name} failed ({result.returncode}); see {logs / (name + '.log')}")

    try:
        source = output / "source"
        source.mkdir()
        names = subprocess.check_output(["git", "ls-files", "--cached", "--others", "--exclude-standard", "-z"], cwd=REPO).decode().split("\0")
        inputs = {}
        for name in sorted(set(names) - {""}):
            parts = Path(name).parts
            if name != "VERSION" and parts[0] not in PREFIXES:
                continue
            if any(p in {"node_modules", "dist", "build", "build-host-tests", "__pycache__"} for p in parts[1:-1]):
                # frontend/build contains version source, unlike CMake's build directories.
                if not (parts[:2] == ("frontend", "build") and len(parts) == 3):
                    continue
            item = REPO / name
            if item.is_symlink() or not item.is_file():
                raise ValueError(f"unsafe or missing build input: {name}")
            target = source / name
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(item, target)
            inputs[name] = sha(target)
        (output / "source-manifest.json").write_text(json.dumps(inputs, indent=2) + "\n")
        (output / "source.patch").write_bytes(subprocess.check_output(["git", "diff", "--binary", "HEAD"], cwd=REPO))
        sdk = Path(env.get("PICO_SDK_PATH", "")).resolve()
        if not (sdk / "external/pico_sdk_import.cmake").is_file():
            raise ValueError("PICO_SDK_PATH must point to an initialized Pico SDK")
        report["pico_sdk_head"] = subprocess.check_output(["git", "-C", str(sdk), "rev-parse", "HEAD"], text=True).strip()
        run("toolchain-go", ["go", "version"])
        run("toolchain-node", ["node", "--version"])
        run("toolchain-npm", ["npm", "--version"])
        run("toolchain-cmake", ["cmake", "--version"])
        run("toolchain-arm", ["arm-none-eabi-gcc", "--version"])
        front = source / "frontend"
        if args.install_deps:
            run("npm-ci", ["npm", "ci", "--cache", output / "npm-cache"], front)
        else:
            modules = REPO / "frontend/node_modules"
            if not modules.is_dir():
                raise ValueError("frontend/node_modules missing; use --install-deps or npm ci first")
            (front / "node_modules").symlink_to(modules, target_is_directory=True)
        run("npm-dependencies", ["npm", "ls", "--all", "--json"], front)
        for module in ["config", "backend", "controller-daemon"]:
            run(module + "-test", ["go", "test", "./..."], source / module)
        run("frontend-test", ["npm", "test"], front)
        run("frontend-lint", ["npm", "run", "lint"], front)
        run("frontend-build", ["npm", "run", "build"], front)
        web = output / "web"
        shutil.copytree(front / "dist", web)
        index = (web / "index.html").read_text()
        refs = re.findall(r'(?:src|href)="(/assets/[^"?#]+)"', index)
        if not refs or any(f"-v{version}-" not in ref or not (web / ref.lstrip("/")).is_file() for ref in refs):
            raise ValueError("Frontend asset version/reference mismatch")
        if not any(f"v{version}" in p.read_text() for p in (web / "assets").glob("*.js")):
            raise ValueError("Frontend compiled version is absent")
        (output / "VERSION").write_text(version + "\n")
        for arch, machine in [("amd64", 62), ("arm64", 183)]:
            for module in ["backend", "controller-daemon"]:
                dest = output / ("linux-" + arch) / "bin" / module
                dest.parent.mkdir(parents=True, exist_ok=True)
                run(module + "-" + arch, ["go", "build", "-trimpath", "-buildvcs=false", "-o", dest, "."], source / module,
                    {"CGO_ENABLED": "0", "GOOS": "linux", "GOARCH": arch})
                data = dest.read_bytes()
                if data[:6] != b"\x7fELF\x02\x01" or struct.unpack_from("<H", data, 18)[0] != machine:
                    raise ValueError(f"wrong ELF architecture: {dest}")
                run(module + "-" + arch + "-metadata", ["go", "version", "-m", dest])
        host = output / "pico-host"
        run("pico-host-configure", ["cmake", "-S", source / "pico-firmware/bedroom-usb/tests", "-B", host])
        run("pico-host-build", ["cmake", "--build", host, "--parallel", "4"])
        run("pico-host-test", ["ctest", "--test-dir", host, "--output-on-failure"])
        firmware = output / "pico-build"
        cmake = ["cmake", "-S", source / "pico-firmware/bedroom-usb", "-B", firmware, "-DPICO_BOARD=pico2", "-DCMAKE_BUILD_TYPE=Release"]
        if env.get("PICOTOOL_DIR"):
            cmake.append("-Dpicotool_DIR=" + env["PICOTOOL_DIR"])
        run("pico-configure", cmake)
        run("pico-build", ["cmake", "--build", firmware, "--parallel", "4"])
        uf2 = firmware / "bedroom_usb.uf2"
        data = uf2.read_bytes()
        if not data or len(data) % 512:
            raise ValueError("invalid UF2 size")
        families = set()
        for offset in range(0, len(data), 512):
            a, b, flags, address, size, block, total, family = struct.unpack_from("<8I", data, offset)
            if (a, b) != (0x0A324655, 0x9E5D5157) or struct.unpack_from("<I", data, offset + 508)[0] != 0x0AB16F30:
                raise ValueError("invalid UF2 block")
            # SDK boot/uf2.h: RP2350 ARM secure plus an absolute-address helper block.
            if not flags & 0x2000 or family not in {0xE48BFF59, 0xE48BFF57}:
                raise ValueError("UF2 contains an unexpected family")
            families.add(family)
        if 0xE48BFF59 not in families:
            raise ValueError("UF2 has no RP2350 ARM secure payload")
        dest = output / "firmware" / f"web-controller-pico2h-v{version}.uf2"
        dest.parent.mkdir()
        shutil.copy2(uf2, dest)
        report["pico_board"] = "pico2"
        report["pico_uf2_families"] = sorted(hex(f) for f in families)
        tool = Path(env.get("PICOTOOL_DIR", "")) / "picotool"
        if tool.is_file():
            run("pico-info", [tool, "info", "-a", dest])
            if f"{version}" not in (logs / "pico-info.log").read_text():
                raise ValueError("UF2 program version metadata mismatch")
        elif (version + "\0").encode() not in data:
            raise ValueError("UF2 version metadata absent")
        # Verify source inputs did not change during the build.
        if any(sha(source / name) != digest for name, digest in inputs.items()):
            raise ValueError("snapshot source changed during build")
        if any(sha(REPO / name) != digest for name, digest in inputs.items()):
            raise ValueError("working source changed during build; rerun with a fresh output")
        for root in [output / "linux-amd64", output / "linux-arm64", web, dest.parent]:
            for path in sorted(root.rglob("*")):
                if path.is_file():
                    report["artifacts"][str(path.relative_to(output))] = sha(path)
        report["build_passed"] = True
        report["note"] = "Build verification only; archives, installation and hardware validation are separate. Dirty snapshot is not a clean release commit."
        print("Build and verification passed: " + str(output), flush=True)
    except Exception as error:
        report["build_passed"] = False
        report["error"] = str(error)
        raise
    finally:
        (output / "build-report.json").write_text(json.dumps(report, indent=2) + "\n")


if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        print(str(error), file=sys.stderr)
        sys.exit(1)
