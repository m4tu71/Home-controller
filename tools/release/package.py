#!/usr/bin/env python3
"""Create and inspect local release assets. Never deploy or publish."""
import argparse
import gzip
import hashlib
import io
import json
from pathlib import Path
import re
import shutil
import struct
import subprocess
import tarfile
import npm_notices

REPO = Path(__file__).resolve().parents[2]
EXAMPLES = ['README.md', 'config.example.json', 'systemd/web-controller-backend.service',
            'systemd/web-controller-daemon.service', 'nginx/web-controller.conf']


def sha(data):
    return hashlib.sha256(data).hexdigest()


def require(ok, message):
    if not ok:
        raise ValueError(message)


def verify_npm_notices(frontend, text):
    expected, packages = npm_notices.render(frontend, text)
    require(expected == text, 'npm runtime notices differ from locked build dependencies; regenerate THIRD_PARTY_NOTICES.md')
    return packages


def inspect_archive(path, expected, version, arch):
    """Inspect all members before extracting; forbid links and unexpected paths."""
    manifest = []
    for name in expected:
        require(not Path(name).is_absolute() and '..' not in Path(name).parts and str(Path(name)) == name, 'unsafe expected path')
    with tarfile.open(path, 'r:gz') as archive:
        members = archive.getmembers()
        names = [m.name for m in members]
        dirs = {f'v{version}'}
        for name in expected:
            dirs.update(str(p) for p in Path(f'v{version}/{name}').parents if str(p) != '.')
        files = {f'v{version}/{name}': data for name, data in expected.items()}
        require(len(names) == len(set(names)), 'duplicate archive member')
        require(set(names) == dirs | set(files), 'unexpected or missing archive member')
        for m in members:
            require(m.uid == m.gid == 0, 'unexpected archive owner')
            if m.name in dirs:
                require(m.isdir() and m.mode == 0o755, 'directory type/mode mismatch')
                continue
            require(m.isfile(), 'links and special files forbidden')
            rel = m.name.split('/', 1)[1]
            require(m.mode == (0o755 if rel.startswith('bin/') else 0o644), 'file mode mismatch')
            data = archive.extractfile(m).read()
            require(data == files[m.name], 'archive content mismatch')
            manifest.append({'path': m.name, 'installed_path': '/opt/web-controller/releases/' + m.name,
                             'mode': oct(m.mode), 'sha256': sha(data), 'size': len(data)})
            if rel.startswith('bin/'):
                require(data[:6] == b'\x7fELF\x02\x01', 'expected ELF64 little endian')
                require(struct.unpack_from('<H', data, 18)[0] == {'amd64': 62, 'arm64': 183}[arch], 'wrong ELF architecture')
        # All members have now been validated; create an isolated extraction tree.
        target = path.parent / 'inspection' / arch / 'opt/web-controller/releases'
        target.mkdir(parents=True)
        for m in members:
            dest = target / m.name
            if m.isdir():
                dest.mkdir(exist_ok=True)
            else:
                dest.write_bytes(archive.extractfile(m).read())
            dest.chmod(m.mode)
        for row in manifest:
            extracted = path.parent / 'inspection' / arch / row['installed_path'].lstrip('/')
            require(sha(extracted.read_bytes()) == row['sha256'], 'extraction hash mismatch')
            require(extracted.stat().st_mode & 0o777 == int(row['mode'], 8), 'extraction mode mismatch')
    return manifest


def create_archive(path, files, version):
    entries = {f'v{version}/{name}': data for name, data in files.items()}
    dirs = {f'v{version}'}
    for name in entries:
        dirs.update(str(p) for p in Path(name).parents if str(p) != '.')
    with path.open('xb') as raw, gzip.GzipFile(filename='', mode='wb', fileobj=raw, mtime=0) as gz:
        with tarfile.open(fileobj=gz, mode='w', format=tarfile.USTAR_FORMAT) as archive:
            for name in sorted(dirs | set(entries)):
                info = tarfile.TarInfo(name)
                info.uid = info.gid = info.mtime = 0
                if name in dirs:
                    info.type, info.mode = tarfile.DIRTYPE, 0o755
                    archive.addfile(info)
                else:
                    info.mode = 0o755 if '/bin/' in name else 0o644
                    info.size = len(entries[name])
                    archive.addfile(info, io.BytesIO(entries[name]))


def package(build, output, allow_missing_license=False, allow_dirty_build=False, require_clean=False):
    output.mkdir(parents=True, exist_ok=False)
    report = {'package_passed': False, 'release_ready': False, 'errors': []}
    try:
        status = subprocess.check_output(['git', 'status', '--porcelain'], cwd=REPO, text=True)
        require(not require_clean or not status, 'clean packaging source required')
        version = (REPO / 'VERSION').read_text().strip()
        require(re.fullmatch(r'\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?', version), 'invalid version')
        previous = json.loads((build / 'build-report.json').read_text())
        require(previous['build_passed'], 'successful build required')
        require(previous['dirty'] is False or allow_dirty_build, 'clean build required; validation only with --allow-dirty-build')
        require(previous['version'] == version, 'build version mismatch')
        source = json.loads((build / 'source-manifest.json').read_text())
        # Reuse binaries only while all production build inputs match that build.
        checked = {}
        for name, digest in source.items():
            if name.startswith('examples/') or name.endswith(('_test.go', '.test.ts', '.test.tsx')):
                continue
            require((REPO / name).is_file() and sha((REPO / name).read_bytes()) == digest, 'source changed since build: ' + name)
            if Path(name).suffix in {'.go', '.ts', '.tsx', '.c', '.cpp', '.h', '.cmake'}:
                data = (REPO / name).read_bytes()
                require(b'/home/' not in data and b'192.168.' not in data and b'usb-Raspberry' not in data, 'individual installation value in source: ' + name)
            checked[name] = digest
        tracked = subprocess.check_output(['git', 'ls-files', '--cached', '--others', '--exclude-standard'], cwd=REPO, text=True).splitlines()
        for name in tracked:
            if name.startswith(('backend/', 'controller-daemon/', 'config/', 'frontend/src/', 'frontend/build/', 'pico-firmware/')):
                if not name.endswith(('_test.go', '.test.ts', '.test.tsx')):
                    require(name in source, 'new build source requires rebuild: ' + name)
        for name, digest in previous['artifacts'].items():
            require(not Path(name).is_absolute() and '..' not in Path(name).parts and str(Path(name)) == name, 'unsafe artifact path')
            data = (build / name).read_bytes()
            require(sha(data) == digest, 'build artifact hash mismatch: ' + name)
            require(b'/home/' not in data and b'192.168.' not in data and b'usb-Raspberry' not in data, 'individual installation value in artifact: ' + name)
        common = {'VERSION': (REPO / 'VERSION').read_bytes(), 'INSTALL.md': (REPO / 'INSTALL.md').read_bytes(),
                  'THIRD_PARTY_NOTICES.md': (REPO / 'THIRD_PARTY_NOTICES.md').read_bytes()}
        runtime_notices = verify_npm_notices(build / 'source/frontend', common['THIRD_PARTY_NOTICES.md'].decode())
        common.update({'examples/' + name: (REPO / 'examples' / name).read_bytes() for name in EXAMPLES})
        licensed = (REPO / 'LICENSE').is_file()
        require(licensed or allow_missing_license, 'LICENSE absent: rights holder decision required; validation only with --allow-missing-license')
        if licensed:
            common['LICENSE'] = (REPO / 'LICENSE').read_bytes()
            require(bool(common['LICENSE'].strip()), 'empty LICENSE')
        # Explicit allowlist, never recursively copy repository/runtime directories.
        web = {name: (build / name).read_bytes() for name in previous['artifacts'] if name.startswith('web/')}
        require('web/index.html' in web and any(n.endswith('.js') for n in web), 'web assets missing')
        text_scan = {}
        for name, data in common.items():
            matches = re.findall(rb'/home/[^\s`]+|\b192\.168\.\d+\.\d+|usb-Raspberry[^\s"`]+', data)
            require(not matches, 'individual installation value in ' + name)
            text_scan[name] = 'passed'
        config = json.loads(common['examples/config.example.json'])
        require(len(config) == 10 and config['pico_serial_path'] == '/dev/serial/by-id/CHANGE_ME', 'example config contract mismatch')
        manifests = {}
        assets = []
        for arch in ['amd64', 'arm64']:
            files = dict(common, **web)
            for binary in ['backend', 'controller-daemon']:
                files['bin/' + binary] = (build / f'linux-{arch}/bin/{binary}').read_bytes()
            path = output / f'web-controller-v{version}-linux-{arch}.tar.gz'
            create_archive(path, files, version)
            manifests[arch] = inspect_archive(path, files, version, arch)
            assets.append(path)
        firmware = output / f'web-controller-pico2h-v{version}.uf2'
        shutil.copyfile(build / 'firmware' / firmware.name, firmware)
        require(sha(firmware.read_bytes()) == previous['artifacts']['firmware/' + firmware.name], 'UF2 hash mismatch')
        assets.append(firmware)
        checksums = {p.name: sha(p.read_bytes()) for p in assets}
        (output / 'SHA256SUMS').write_text(''.join(f'{digest}  {name}\n' for name, digest in sorted(checksums.items())))
        for line in (output / 'SHA256SUMS').read_text().splitlines():
            digest, name = line.split('  ')
            require(sha((output / name).read_bytes()) == digest, 'checksum readback failed')
        report.update(package_passed=True, version=version, build_git_head=previous['git_head'],
                      packaging_git_head=subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=REPO, text=True).strip(),
                      packaging_status=subprocess.check_output(['git', 'status', '--porcelain'], cwd=REPO, text=True),
                      license_included=licensed, build_dirty=previous['dirty'], manifests=manifests, checksums=checksums,
                      build_input_hashes=checked, package_input_hashes={n: sha(d) for n, d in common.items()},
                      npm_runtime_notices=runtime_notices,
                      config_scan=text_scan,
                      limitations=['License selection/approval required' if not licensed else 'License included; publication approval remains manual',
                                   'Packaging checks archive contents; deployment, runtime/hardware verification status and publication approval are not assessed',
                                   'Final clean commit rebuild/repackage required' if previous['dirty'] else 'Clean build inputs verified'])
    except Exception as error:
        report['errors'].append(str(error))
        raise
    finally:
        (output / 'package-report.json').write_text(json.dumps(report, ensure_ascii=False, indent=2) + '\n')
    return report


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--build', required=True, type=Path)
    parser.add_argument('--output', required=True, type=Path)
    parser.add_argument('--allow-missing-license', action='store_true', help='local inspection only; never a public release')
    parser.add_argument('--allow-dirty-build', action='store_true', help='local inspection only; require clean rebuild before release')
    parser.add_argument('--require-clean', action='store_true', help='reject uncommitted packaging inputs for final candidates')
    args = parser.parse_args()
    report = package(args.build.resolve(), args.output.resolve(), args.allow_missing_license, args.allow_dirty_build, args.require_clean)
    print(json.dumps({k: report[k] for k in ['package_passed', 'release_ready', 'license_included']}, ensure_ascii=False))


if __name__ == '__main__':
    main()
