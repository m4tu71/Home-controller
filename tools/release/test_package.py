"""Archive contract and rejection tests; no services or production data."""
import io
import json
from pathlib import Path
import struct
import tarfile
import tempfile
import unittest
from unittest.mock import patch
import package as release


class ArchiveTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        data = bytearray(64)
        data[:6] = b'\x7fELF\x02\x01'
        struct.pack_into('<H', data, 18, 62)
        self.files = {'bin/backend': bytes(data), 'bin/controller-daemon': bytes(data),
                      'VERSION': b'1.0.0\n', 'web/index.html': b'<html></html>'}
        self.path = self.root / 'candidate.tar.gz'

    def test_package_report_does_not_infer_external_gate_status_or_publication(self):
        repo, build = self.root / 'repo', self.root / 'build'
        repo.mkdir()
        build.mkdir()
        for name in ['VERSION', 'INSTALL.md', 'THIRD_PARTY_NOTICES.md', 'LICENSE']:
            (repo / name).write_text('1.0.0\n' if name == 'VERSION' else 'fixture\n')
        for name in release.EXAMPLES:
            path = repo / 'examples' / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text('fixture\n')
        config = {'pico_serial_path': '/dev/serial/by-id/CHANGE_ME',
                  **{str(i): i for i in range(9)}}
        (repo / 'examples/config.example.json').write_text(json.dumps(config))
        artifacts = {'web/index.html': b'<html></html>',
                     'web/assets/index.js': b'fixture',
                     'firmware/web-controller-pico2h-v1.0.0.uf2': b'fixture'}
        for arch, machine in [('amd64', 62), ('arm64', 183)]:
            for binary in ['backend', 'controller-daemon']:
                data = bytearray(self.files['bin/backend'])
                struct.pack_into('<H', data, 18, machine)
                artifacts[f'linux-{arch}/bin/{binary}'] = bytes(data)
        for name, data in artifacts.items():
            path = build / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(data)
        (build / 'source-manifest.json').write_text('{}')

        def git_result(argv, **kwargs):
            if argv[1] in ('status', 'ls-files'):
                return ''
            return 'fixture-commit\n'

        for dirty in [False, True]:
            (build / 'build-report.json').write_text(json.dumps({
                'build_passed': True, 'dirty': dirty, 'version': '1.0.0',
                'git_head': 'fixture-build',
                'artifacts': {name: release.sha(data) for name, data in artifacts.items()}}))
            with patch.object(release, 'REPO', repo), \
                    patch.object(release.subprocess, 'check_output', side_effect=git_result), \
                    patch.object(release, 'verify_npm_notices', return_value=[]):
                report = release.package(build, self.root / f'package-{dirty}', allow_dirty_build=dirty)
            self.assertTrue(report['package_passed'])
            self.assertFalse(report['release_ready'])
            self.assertEqual(report['build_dirty'], dirty)
            self.assertEqual(report['errors'], [])
            self.assertFalse(any('gates remain' in text or 'Step 4G' in text for text in report['limitations']))
            self.assertTrue(any('not assessed' in text for text in report['limitations']))

    def test_stale_npm_notices_rejected_before_packaging(self):
        front = self.root / 'frontend'
        package = front / 'node_modules/runtime'
        package.mkdir(parents=True)
        (package / 'package.json').write_text('{"name":"runtime","version":"2.0.0","license":"MIT"}')
        (package / 'LICENSE').write_text('MIT License\nRuntime copyright\n')
        (front / 'package-lock.json').write_text(json.dumps({'packages': {'': {}, 'node_modules/runtime': {'version': '2.0.0'}}}))
        old = '# Notices\n\n## runtime 1.0.0\nold\n\n## go.bug.st/serial v1\nGo notice\n'
        with self.assertRaisesRegex(ValueError, 'notices differ'):
            release.verify_npm_notices(front, old)
        updated, _ = release.npm_notices.render(front, old)
        self.assertEqual(release.verify_npm_notices(front, updated), [{'name': 'runtime', 'version': '2.0.0'}])

    def test_roundtrip_modes_paths_and_determinism(self):
        release.create_archive(self.path, self.files, '1.0.0')
        other = self.root / 'again.tar.gz'
        release.create_archive(other, self.files, '1.0.0')
        self.assertEqual(self.path.read_bytes(), other.read_bytes())
        rows = release.inspect_archive(self.path, self.files, '1.0.0', 'amd64')
        self.assertEqual(len(rows), 4)
        self.assertTrue(all(r['installed_path'].startswith('/opt/web-controller/releases/v1.0.0/') for r in rows))

    def test_wrong_architecture(self):
        release.create_archive(self.path, self.files, '1.0.0')
        with self.assertRaisesRegex(ValueError, 'architecture'):
            release.inspect_archive(self.path, self.files, '1.0.0', 'arm64')

    def test_changed_content(self):
        release.create_archive(self.path, self.files, '1.0.0')
        self.files['VERSION'] = b'2.0.0\n'
        with self.assertRaisesRegex(ValueError, 'content'):
            release.inspect_archive(self.path, self.files, '1.0.0', 'amd64')

    def malformed(self, names, mode=0o644, symlink=False):
        with tarfile.open(self.path, 'w:gz') as archive:
            for name in names:
                info = tarfile.TarInfo(name)
                info.mode = mode
                if symlink:
                    info.type, info.linkname = tarfile.SYMTYPE, '/etc/passwd'
                archive.addfile(info, io.BytesIO(b''))

    def test_traversal(self):
        self.malformed(['../escape'])
        with self.assertRaisesRegex(ValueError, 'unexpected'):
            release.inspect_archive(self.path, self.files, '1.0.0', 'amd64')
        self.assertFalse((self.root.parent / 'escape').exists())

    def test_unsafe_allowlist(self):
        release.create_archive(self.path, self.files, '1.0.0')
        with self.assertRaisesRegex(ValueError, 'unsafe expected path'):
            release.inspect_archive(self.path, {'../escape': b''}, '1.0.0', 'amd64')

    def test_duplicate(self):
        self.malformed(['v1.0.0/VERSION', 'v1.0.0/VERSION'])
        with self.assertRaisesRegex(ValueError, 'duplicate'):
            release.inspect_archive(self.path, self.files, '1.0.0', 'amd64')

    def test_symlink_and_mode(self):
        for symlink in [True, False]:
            path = self.root / f'{symlink}.tar.gz'
            release.create_archive(path, self.files, '1.0.0')
            with tarfile.open(path) as archive:
                entries = [(m, archive.extractfile(m).read() if m.isfile() else None) for m in archive]
            with tarfile.open(self.path, 'w:gz') as archive:
                for m, data in entries:
                    if m.name == 'v1.0.0/bin/backend':
                        if symlink:
                            m.type, m.linkname, m.size = tarfile.SYMTYPE, '/etc/passwd', 0
                            data = None
                        else:
                            m.mode = 0o777
                    archive.addfile(m, io.BytesIO(data) if data is not None else None)
            with self.assertRaises(ValueError):
                release.inspect_archive(self.path, self.files, '1.0.0', 'amd64')
            self.assertFalse((self.root / 'inspection').exists())


if __name__ == '__main__':
    unittest.main()
