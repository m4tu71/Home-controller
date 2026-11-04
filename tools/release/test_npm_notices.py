import importlib.util
import json
from pathlib import Path
import tempfile
import unittest


spec = importlib.util.spec_from_file_location('npm_notices', Path(__file__).with_name('npm_notices.py'))
notices = importlib.util.module_from_spec(spec)
spec.loader.exec_module(notices)


class NoticeTests(unittest.TestCase):
    def fixture(self, root):
        package = root / 'node_modules/runtime'
        package.mkdir(parents=True)
        (package / 'package.json').write_text(json.dumps({'name': 'runtime', 'version': '2.0.0', 'license': 'MIT'}))
        (package / 'LICENSE').write_text('MIT License\nRuntime copyright\n')
        (root / 'package-lock.json').write_text(json.dumps({'packages': {'': {},
            'node_modules/runtime': {'version': '2.0.0'}, 'node_modules/build-only': {'version': '1.0', 'dev': True}}}))
        return package

    def test_runtime_only_and_existing_go_pico_tail_preserved(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            self.fixture(root)
            original = '# Notices\n\n## Old npm 1.0\nold\n\n## go.bug.st/serial v1.6.4\nexisting Go and Pico text\n'
            result, rows = notices.render(root, original)
            self.assertIn('## runtime 2.0.0\n\nMIT License\nRuntime copyright', result)
            self.assertNotIn('build-only', result)
            self.assertTrue(result.endswith(original[original.index(notices.TAIL):]))
            self.assertEqual(rows, [{'name': 'runtime', 'version': '2.0.0'}])
            self.assertEqual(notices.render(root, result)[0], result)

    def test_mismatched_installed_version_stops_generation(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            package = self.fixture(root)
            (package / 'package.json').write_text('{"name":"runtime","version":"1.0","license":"MIT"}')
            with self.assertRaisesRegex(ValueError, 'differs from lock'):
                notices.render(root, '# Notices\n## go.bug.st/serial v1\noriginal\n')

    def test_missing_license_stops_generation(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            package = self.fixture(root)
            (package / 'LICENSE').rename(package / 'other')
            with self.assertRaisesRegex(ValueError, 'license missing'):
                notices.render(root, '# Notices\n## go.bug.st/serial v1\noriginal\n')


if __name__ == '__main__':
    unittest.main()
