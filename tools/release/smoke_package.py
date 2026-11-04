#!/usr/bin/env python3
"""Finite amd64 package smoke test using generated fixtures, no systemd/Nginx/real data/IR."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import socket
import subprocess
import tempfile
import time
import urllib.error
import urllib.request


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--release-root', type=Path, required=True,
                        help='Already inspected native release in the project artifact directory')
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    release = args.release_root.resolve(strict=True)
    output = args.output.resolve()
    project = Path(__file__).resolve().parents[2]
    artifacts = project / '.artifacts'
    if not release.is_relative_to(artifacts) or not output.is_relative_to(artifacts):
        parser.error('Use isolated project .artifacts directories only')
    if os.uname().machine != 'x86_64':
        parser.error('This smoke test expects native amd64')
    output.mkdir(mode=0o700, parents=True, exist_ok=False)
    os.chmod(output, 0o700)
    fixture = Path(tempfile.mkdtemp(prefix='home-controller-smoke-'))
    report = {'passed': False, 'errors': [], 'checks': [], 'fixture': str(fixture),
              'scope': 'native package processes and generated Room fixture only; no systemd/Nginx/browser/real data/IR'}
    processes, logs = [], []
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    try:
        with socket.socket() as probe:
            probe.bind(('127.0.0.1', 0))
            port = probe.getsockname()[1]
        for name in ('data', 'maintenance', 'backups', 'trash', 'socket', 'maintenance/plans', 'maintenance/operations'):
            path = fixture / name
            path.mkdir(mode=0o750 if name == 'socket' else 0o700)
        serial = Path('/dev/serial/by-id/home-controller-smoke-no-device')
        if os.path.lexists(serial):
            raise ValueError('Fake serial path unexpectedly exists')
        cfg = {'http_listen': '127.0.0.1:' + str(port),
               'data_root': str(fixture / 'data'), 'maintenance_root': str(fixture / 'maintenance'),
               'backup_root': str(fixture / 'backups'), 'trash_root': str(fixture / 'trash'),
               'trash_retention_days': 30, 'software_root': str(release), 'web_root': str(release / 'web'),
               'socket_path': str(fixture / 'socket/controller.sock'), 'pico_serial_path': str(serial)}
        config = fixture / 'config.json'
        config.write_text(json.dumps(cfg))
        config.chmod(0o600)
        env = {key: value for key, value in os.environ.items() if key not in
               ('WEB_CONTROLLER_MAINTENANCE_STATE_DIR', 'WEB_CONTROLLER_BACKUP_DIR',
                'WEB_CONTROLLER_TRASH_DIR', 'WEB_CONTROLLER_TRASH_RETENTION_DAYS')}
        for name in ('controller-daemon', 'backend'):
            log = (output / (name + '.log')).open('x')
            os.chmod(log.name, 0o600)
            logs.append(log)
            binary = release / 'bin' / name
            processes.append(subprocess.Popen([str(binary), '--config', str(config)], env=env, stdout=log, stderr=subprocess.STDOUT))
            report.setdefault('binary_sha256', {})[name] = hashlib.sha256(binary.read_bytes()).hexdigest()
        def request(path, method='GET', body=None):
            for process in processes:
                if process.poll() is not None:
                    raise ValueError('Package process exited unexpectedly')
            request = urllib.request.Request('http://127.0.0.1:' + str(port) + path,
                       data=None if body is None else json.dumps(body).encode(), method=method,
                       headers={'Content-Type': 'application/json'} if body is not None else {})
            try:
                with opener.open(request, timeout=5) as response:
                    return response.status, json.load(response)
            except urllib.error.HTTPError as response:
                return response.code, json.load(response)
        def check(name, value):
            report['checks'].append({'name': name, 'passed': bool(value)})
            if not value:
                raise ValueError('Smoke check failed: ' + name)
        ready = False
        for attempt in range(40):
            try:
                status, health = request('/api/health')
                if status == 200 and health.get('ok'):
                    ready = True
                    break
            except (OSError, ValueError):
                pass
            time.sleep(0.25)
        check('health', ready)
        status, body = request('/api/health/controller')
        check('daemon_reached_without_pico', status == 503 and body.get('daemonOk') is False
              and 'failed to open serial port' in body.get('error', ''))
        status, body = request('/api/controller/rooms')
        check('initial_empty_rooms', status == 200 and body == [])
        room = {'id': 'release_smoke_fixture', 'name': 'Release smoke fixture'}
        status, body = request('/api/controller/rooms', 'POST', room)
        check('create_room', status == 201 and body == room)
        status, body = request('/api/controller/rooms/' + room['id'])
        check('room_http_and_disk_readback', status == 200 and body == room
              and json.loads((fixture / 'data/rooms' / room['id'] / 'room.json').read_text()) == room)
        index = (release / 'web/index.html').read_text()
        refs = re.findall(r'(?:src|href)="(/assets/[^"?#]+)"', index)
        version = (release / 'VERSION').read_text().strip()
        check('versioned_web_assets', bool(refs) and all('-v' + version + '-' in ref
              and (release / 'web' / ref.lstrip('/')).is_file() for ref in refs))
        report['passed'] = True
    except Exception as error:
        report['errors'].append(type(error).__name__ + ': ' + str(error))
    finally:
        for process in reversed(processes):
            if process.poll() is None:
                process.terminate()
                try:
                    process.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait(timeout=5)
        for log in logs:
            log.close()
        target = output / 'report.json'
        target.write_text(json.dumps(report, ensure_ascii=False, indent=2) + '\n')
        target.chmod(0o600)
    print(json.dumps({'passed': report['passed'], 'checks': len(report['checks']), 'errors': report['errors'],
                      'report': str(output / 'report.json')}, ensure_ascii=False))
    return int(not report['passed'])


if __name__ == '__main__':
    raise SystemExit(main())
