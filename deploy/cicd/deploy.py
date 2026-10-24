#!/usr/bin/env python3
"""Deploy one service of the existing split-VM installation. Python stdlib only."""
import argparse
from contextlib import contextmanager
import fcntl
import json
import os
from pathlib import Path
import re
import socket
import subprocess
import sys
import tempfile
import time
import urllib.request

SERVICES = ('frontend', 'backend')
CONFIG = Path('/etc/standard-setting-cicd/config.json')


class DeploymentError(RuntimeError):
    pass


class CommandFailure(DeploymentError):
    def __init__(self, args, code, stdout, stderr):
        super().__init__(f"Command failed (exit {code}): {' '.join(args[:3])}. "
                         'Private diagnostics: /var/lib/standard-setting-cicd/last-error.json')
        self.details = {'command': args, 'exit_code': code, 'stdout': stdout, 'stderr': stderr}


def command(args, input=None):
    # Never print stdin (registry token), Compose config, or container environment.
    result = subprocess.run(args, input=input, text=True, capture_output=True)
    if result.returncode:
        raise CommandFailure(args, result.returncode, result.stdout[-32768:], result.stderr[-32768:])
    return result.stdout.strip()


def endpoint_health(service):
    url = {'frontend': 'http://127.0.0.1:3000/login',
           'backend': 'http://127.0.0.1:8080/api/v1/health'}[service]
    try:
        with urllib.request.urlopen(url, timeout=15) as response:
            if response.status != 200:
                raise DeploymentError(f'{service} endpoint returned {response.status}')
    except Exception as error:
        raise DeploymentError(f'{service} endpoint health check failed') from error


def atomic_json(path, data):
    descriptor, name = tempfile.mkstemp(prefix='.' + path.name + '-', dir=path.parent)
    try:
        with os.fdopen(descriptor, 'w') as stream:
            json.dump(data, stream, indent=2)
            stream.write('\n')
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(name, path)
    finally:
        if os.path.exists(name):
            os.unlink(name)


class Deployer:
    def __init__(self, config, runner=command, health=endpoint_health):
        self.directory = Path(config['deploy_dir'])
        self.state_dir = Path(config['state_dir'])
        self.prefix = config['registry_prefix']
        self.hostname = config['expected_hostname']
        if not self.directory.is_absolute() or not self.state_dir.is_absolute():
            raise DeploymentError('Configuration paths must be absolute')
        if not re.fullmatch(r'[a-z0-9-]+-docker\.pkg\.dev/[a-z0-9-]+/[a-z0-9_-]+', self.prefix):
            raise DeploymentError('Invalid Artifact Registry prefix')
        self.runner, self.health = runner, health
        self.state_file = self.state_dir / 'state.json'
        self.overlay = self.state_dir / 'images.json'
        self.docker_config = None

    @contextmanager
    def locked(self):
        if socket.gethostname().split('.')[0] != self.hostname:
            raise DeploymentError(f'Expected application VM {self.hostname}')
        for name in ('compose.yaml', 'compose.remote-data.yaml', '.env.remote'):
            if not (self.directory / name).is_file():
                raise DeploymentError(f'Missing deployment file: {name}')
        self.state_dir.mkdir(mode=0o700, parents=True, exist_ok=True)
        with (self.state_dir / 'deploy.lock').open('a') as lock:
            deadline = time.monotonic() + 600
            while True:
                try:
                    fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
                    break
                except BlockingIOError:
                    if time.monotonic() >= deadline:
                        raise DeploymentError('Timed out waiting for shared VM deployment lock')
                    time.sleep(0.25)
            try:
                yield
            finally:
                fcntl.flock(lock, fcntl.LOCK_UN)

    def docker(self, *args, input=None):
        prefix = ['docker']
        if self.docker_config:
            prefix += ['--config', self.docker_config]
        try:
            return self.runner(prefix + list(args), input=input)
        except CommandFailure as error:
            # Diagnostics can contain application output: keep them root-only on VM.
            # The registry token is passed only on stdin and is never recorded.
            atomic_json(self.state_dir / 'last-error.json', error.details)
            raise

    def compose(self, *args, overlay=None):
        flags = ['compose', '--project-directory', str(self.directory),
                 '--env-file', str(self.directory / '.env.remote'),
                 '-f', str(self.directory / 'compose.yaml'),
                 '-f', str(self.directory / 'compose.remote-data.yaml')]
        if overlay is not None:
            flags += ['-f', str(overlay)]
        return self.docker(*(flags + list(args)))

    def load(self):
        if not self.state_file.is_file():
            raise DeploymentError('Run ss-deploy init before enabling GitHub deployment')
        try:
            state = json.loads(self.state_file.read_text())
            if state['version'] != 1 or set(state['services']) != set(SERVICES):
                raise ValueError('invalid state version/services')
            for service in SERVICES:
                release = state['services'][service]
                if not isinstance(release['run_number'], int) or not release['image']:
                    raise ValueError('invalid release state')
            if 'pending' not in state:
                raise ValueError('missing pending state')
            return state
        except (KeyError, TypeError, ValueError) as error:
            raise DeploymentError('Invalid release state; investigate before continuing') from error

    @staticmethod
    def images(state):
        return {'services': {service: {'image': state['services'][service]['image']}
                             for service in SERVICES}}

    def init(self):
        with self.locked():
            if self.state_file.exists():
                self.load()
                print('CI/CD already initialized; existing release state preserved.')
                return
            if self.overlay.exists():
                raise DeploymentError('Overlay without state; investigate interrupted initialization')
            self.compose('config', '--quiet')
            state = {'version': 1, 'services': {}, 'pending': None}
            for service in SERVICES:
                cid = self.compose('ps', '-q', service)
                if not cid or '\n' in cid:
                    raise DeploymentError(f'Expected one running {service} container')
                template = ('{"image":{{json .Image}},"running":{{json .State.Running}},'
                            '"health":{{if .State.Health}}{{json .State.Health.Status}}'
                            '{{else}}null{{end}}}')
                running = json.loads(self.docker('inspect', '--format', template, cid))
                if running['running'] is not True or running['health'] != 'healthy':
                    raise DeploymentError(f'{service} must already be running and healthy')
                if not re.fullmatch(r'sha256:[a-f0-9]{64}', running['image']):
                    raise DeploymentError('Unexpected Docker image ID')
                self.health(service)
                baseline = f"standard-setting-{service}:cicd-initial-{running['image'][7:]}"
                self.docker('image', 'tag', running['image'], baseline)
                state['services'][service] = {'image': baseline, 'commit': None,
                                               'run_number': 0, 'previous': None}
            atomic_json(self.overlay, self.images(state))
            atomic_json(self.state_file, state)
            print('Adopted both healthy running images. No containers restarted.')

    def validate_release(self, service, image, commit, run_number):
        if service not in SERVICES:
            raise DeploymentError('Service must be frontend or backend')
        if not re.fullmatch(re.escape(self.prefix + '/' + service) + r'@sha256:[a-f0-9]{64}', image):
            raise DeploymentError('Image must be a digest of the configured registry and service')
        if not re.fullmatch(r'[a-f0-9]{40}', commit) or not isinstance(run_number, int) or run_number < 1:
            raise DeploymentError('Invalid commit SHA or workflow run number')

    def deploy(self, service, image, commit, run_number, token):
        self.validate_release(service, image, commit, run_number)
        with self.locked():
            state = self.load()
            if state['pending']:
                raise DeploymentError('Unresolved deployment; inspect status and use explicit rollback first')
            if run_number <= state['services'][service]['run_number']:
                print(f'Skipped stale/already deployed {service} run {run_number}.')
                return
            if not token or '\n' in token.strip('\n') or len(token) > 16384:
                raise DeploymentError('Expected one short-lived registry access token on stdin')
            atomic_json(self.overlay, self.images(state))
            with tempfile.TemporaryDirectory(prefix='ss-registry-') as docker_config:
                self.docker_config = docker_config
                try:
                    self.docker('login', '--username', 'oauth2accesstoken', '--password-stdin',
                                self.prefix.split('/')[0], input=token.rstrip('\n') + '\n')
                    self.docker('pull', image)
                    pending = {'service': service, 'image': image, 'commit': commit,
                               'run_number': run_number, 'phase': 'migration' if service == 'backend' else 'apply',
                               'failed': False}
                    state['pending'] = pending
                    atomic_json(self.state_file, state)
                    candidate = self.images(state)
                    candidate['services'][service]['image'] = image
                    candidate_file = self.state_dir / 'candidate.json'
                    atomic_json(candidate_file, candidate)
                    try:
                        self.compose('config', '--quiet', overlay=candidate_file)
                        if service == 'backend':
                            for operation in ('status', 'up'):
                                self.compose('run', '--rm', '--no-deps', '--pull', 'never',
                                             '--entrypoint', 'migrate', 'backend', '-command', operation,
                                             '-timeout', '2m', overlay=candidate_file)
                        pending['phase'] = 'apply'
                        atomic_json(self.state_file, state)
                        atomic_json(self.overlay, candidate)
                        self.compose('up', '-d', '--no-build', '--no-deps', '--pull', 'never',
                                     '--wait', '--wait-timeout', '180', service, overlay=self.overlay)
                        pending['phase'] = 'health'
                        atomic_json(self.state_file, state)
                        self.health(service)
                        previous = {k: v for k, v in state['services'][service].items() if k != 'previous'}
                        state['services'][service] = {'image': image, 'commit': commit,
                                                       'run_number': run_number, 'previous': previous}
                        state['pending'] = None
                        atomic_json(self.state_file, state)
                    except Exception:
                        if state['pending']:
                            state['pending']['failed'] = True
                            atomic_json(self.state_file, state)
                        raise
                    finally:
                        candidate_file.unlink(missing_ok=True)
                finally:
                    self.docker_config = None
            print(f'Deployed healthy {service}: commit {commit}, run {run_number}, image {image}')

    def rollback(self, service, confirm_schema_compatible=False):
        if service not in SERVICES:
            raise DeploymentError('Service must be frontend or backend')
        if service == 'backend' and not confirm_schema_compatible:
            raise DeploymentError('Backend rollback requires --confirm-schema-compatible; schema is not rolled back')
        with self.locked():
            state = self.load()
            pending = state['pending']
            if pending and pending['service'] != service:
                raise DeploymentError('Resolve the pending deployment of the other service first')
            current = state['services'][service]
            if pending and pending['phase'] == 'rollback':
                # Resume the same rollback after a crash or failed healthcheck.
                # Choosing current here would silently undo the operator's target.
                target = pending.get('target')
                previous_after_success = pending.get('previous_after_success')
            elif pending:
                # Recover an unsuccessful deploy to the last known healthy image.
                target = current
                previous_after_success = current.get('previous')
            else:
                target = current['previous']
                previous_after_success = {k: v for k, v in current.items() if k != 'previous'}
            if not target:
                raise DeploymentError('No previous healthy release to roll back to')
            self.docker('image', 'inspect', '--format', '{{.Id}}', target['image'])
            state['pending'] = {
                'service': service, 'phase': 'rollback', 'failed': False,
                'target': {k: v for k, v in target.items() if k != 'previous'},
                'previous_after_success': previous_after_success,
            }
            atomic_json(self.state_file, state)
            images = self.images(state)
            images['services'][service]['image'] = target['image']
            atomic_json(self.overlay, images)
            try:
                self.compose('up', '-d', '--no-build', '--no-deps', '--pull', 'never',
                             '--wait', '--wait-timeout', '180', service, overlay=self.overlay)
                self.health(service)
                state['services'][service] = dict(target, run_number=current['run_number'],
                                                   previous=previous_after_success)
                state['pending'] = None
                atomic_json(self.state_file, state)
            except Exception:
                state['pending']['failed'] = True
                atomic_json(self.state_file, state)
                raise
            print(f'Rolled back healthy {service}. No database schema/data rollback was performed.')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest='operation', required=True)
    commands.add_parser('init')
    commands.add_parser('status')
    deploy = commands.add_parser('deploy')
    deploy.add_argument('service', choices=SERVICES)
    deploy.add_argument('image')
    deploy.add_argument('commit')
    deploy.add_argument('run_number', type=int)
    rollback = commands.add_parser('rollback')
    rollback.add_argument('service', choices=SERVICES)
    rollback.add_argument('--confirm-schema-compatible', action='store_true')
    args = parser.parse_args()
    if os.geteuid() != 0:
        raise DeploymentError('Run using sudo -n /usr/local/sbin/ss-deploy')
    deployer = Deployer(json.loads(CONFIG.read_text()))
    if args.operation == 'init':
        deployer.init()
    elif args.operation == 'status':
        with deployer.locked():
            print(json.dumps(deployer.load(), indent=2))
    elif args.operation == 'deploy':
        token = sys.stdin.readline(16385)
        deployer.deploy(args.service, args.image, args.commit, args.run_number, token)
    else:
        deployer.rollback(args.service, args.confirm_schema_compatible)


if __name__ == '__main__':
    try:
        main()
    except (DeploymentError, OSError, ValueError, KeyError) as error:
        print(f'ss-deploy: {error}', file=sys.stderr)
        sys.exit(1)
