#!/usr/bin/env python3
"""Refresh bundled npm runtime notices from the lockfile and matching installed licenses."""

import argparse
import json
from pathlib import Path, PurePosixPath


LABELS = {'react': 'React', 'react-dom': 'React DOM', 'scheduler': 'Scheduler',
          'react-router': 'React Router', 'react-router-dom': 'React Router DOM'}
ORDER = ('react', 'react-dom', 'scheduler', 'react-router', 'react-router-dom')
TAIL = '## go.bug.st/serial '


def render(frontend, existing):
    lock = json.loads((frontend / 'package-lock.json').read_text())
    modules = (frontend / 'node_modules').resolve(strict=True)
    if existing.count(TAIL) != 1:
        raise ValueError('Expected one existing Go/Pico notice boundary')
    intro = existing.split('\n## ', 1)[0].rstrip()
    tail = existing[existing.index(TAIL):]
    rows = []
    for relative, entry in lock['packages'].items():
        if not relative or entry.get('dev'):
            continue
        parts = PurePosixPath(relative)
        if parts.is_absolute() or '..' in parts.parts or not relative.startswith('node_modules/'):
            raise ValueError('Unsafe npm package path: ' + relative)
        package = (frontend / relative).resolve(strict=True)
        if not package.is_relative_to(modules):
            raise ValueError('Runtime dependency resolves outside node_modules: ' + relative)
        meta = json.loads((package / 'package.json').read_text())
        if meta.get('version') != entry.get('version') or not meta.get('name') or not meta.get('license'):
            raise ValueError('Runtime package metadata differs from lock: ' + relative)
        licenses = [package / name for name in ('LICENSE', 'LICENSE.md', 'LICENSE.txt', 'license', 'license.md')
                    if (package / name).is_file()]
        if not licenses:
            raise ValueError('Runtime package license missing: ' + relative)
        license_file = licenses[0].resolve(strict=True)
        if not license_file.is_relative_to(package):
            raise ValueError('License resolves outside package: ' + relative)
        text = license_file.read_text().strip()
        if not text:
            raise ValueError('Empty runtime license: ' + relative)
        notices = []
        for name in ('NOTICE', 'NOTICE.md', 'NOTICE.txt'):
            path = package / name
            if path.is_file():
                if not path.resolve(strict=True).is_relative_to(package):
                    raise ValueError('Notice resolves outside package: ' + relative)
                notices.append(path.read_text().strip())
        name = meta['name']
        rows.append((name, entry['version'], text, notices))
    rows.sort(key=lambda row: (ORDER.index(row[0]) if row[0] in ORDER else len(ORDER), row[0], row[1]))
    chunks = [intro]
    for name, version, license_text, notices in rows:
        chunks.append('## ' + LABELS.get(name, name) + ' ' + version + '\n\n' + license_text
                      + ''.join('\n\n' + notice for notice in notices if notice))
    chunks.append(tail.rstrip())
    return '\n\n\n'.join(chunks) + '\n', [{'name': row[0], 'version': row[1]} for row in rows]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--frontend', type=Path, required=True)
    parser.add_argument('--notices', type=Path, required=True)
    parser.add_argument('--check', action='store_true')
    args = parser.parse_args()
    original = args.notices.read_text()
    updated, rows = render(args.frontend, original)
    if args.check:
        if original != updated:
            raise SystemExit('npm runtime notices differ from installed locked packages')
    else:
        args.notices.write_text(updated)
    print(json.dumps({'runtime_packages': rows, 'check': args.check}, ensure_ascii=False))


if __name__ == '__main__':
    main()
