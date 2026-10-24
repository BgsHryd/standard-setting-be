import importlib.util
from contextlib import redirect_stdout
import io
import json
from pathlib import Path
import socket
import tempfile
import threading
import unittest

SCRIPT = Path(__file__).resolve().parents[1] / 'deploy.py'
SPEC = importlib.util.spec_from_file_location('ss_deploy', SCRIPT)
module = importlib.util.module_from_spec(SPEC)
# Missing implementation must be observed before this test can pass.
SPEC.loader.exec_module(module)


class FakeDocker:
    def __init__(self):
        self.calls = []
        self.fail_on = None

    def __call__(self, args, input=None):
        self.calls.append((args, input))
        if self.fail_on and self.fail_on(args):
            raise module.DeploymentError('simulated command failure')
        if 'ps' in args:
            return 'cid-' + args[-1]
        if 'inspect' in args and '--format' in args:
            if args[-1].startswith('cid-'):
                return json.dumps({'image': 'sha256:' + 'a' * 64,
                                   'running': True, 'health': 'healthy'})
            return 'sha256:' + 'a' * 64
        return ''


class DeployTest(unittest.TestCase):
    def setUp(self):
        self.output = io.StringIO()
        self.enterContext(redirect_stdout(self.output))
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.deploy_dir = self.root / 'deploy'
        self.deploy_dir.mkdir()
        for name in ('compose.yaml', 'compose.remote-data.yaml', '.env.remote'):
            (self.deploy_dir / name).write_text('fixture')
        self.cfg = {'deploy_dir': str(self.deploy_dir),
                    'state_dir': str(self.root / 'state'),
                    'registry_prefix': 'asia-southeast2-docker.pkg.dev/test-project/ss-apps',
                    'expected_hostname': socket.gethostname().split('.')[0]}
        self.runner = FakeDocker()
        self.health_failure = False
        self.deployer = module.Deployer(self.cfg, self.runner, self.health)
        self.deployer.init()
        self.runner.calls.clear()

    def health(self, service):
        if self.health_failure:
            raise module.DeploymentError('simulated unhealthy endpoint')

    def state(self):
        return json.loads((self.root / 'state/state.json').read_text())

    def overlay(self):
        return json.loads((self.root / 'state/images.json').read_text())

    def image(self, service, character='b'):
        return self.cfg['registry_prefix'] + '/' + service + '@sha256:' + character * 64

    def deploy(self, service='frontend', run=1, character='b'):
        return self.deployer.deploy(service, self.image(service, character), 'c' * 40, run, 'secret-token')

    def test_frontend_preserves_backend_and_never_starts_dependencies(self):
        backend = self.state()['services']['backend']['image']
        self.deploy()
        self.assertEqual(self.overlay()['services']['backend']['image'], backend)
        commands = [args for args, _ in self.runner.calls]
        ups = [a for a in commands if 'up' in a]
        self.assertEqual(len(ups), 1)
        self.assertIn('--no-deps', ups[0])
        self.assertEqual(ups[0][-1], 'frontend')
        self.assertFalse(any('--entrypoint' in a for a in commands))
        self.assertIsNone(self.state()['pending'])

    def test_backend_migration_precedes_restart_and_never_seeds(self):
        self.deploy('backend')
        commands = [args for args, _ in self.runner.calls]
        migrations = [a for a in commands if '--entrypoint' in a]
        self.assertEqual([a[a.index('-command') + 1] for a in migrations], ['status', 'up'])
        restart = next(a for a in commands if '--wait' in a)
        self.assertLess(commands.index(migrations[-1]), commands.index(restart))
        self.assertNotIn('seed', sum(commands, []))

    def test_stale_run_does_not_pull_or_deploy(self):
        self.deploy(run=3)
        self.runner.calls.clear()
        self.deploy(run=2, character='d')
        self.assertEqual(self.runner.calls, [])
        self.assertEqual(self.state()['services']['frontend']['run_number'], 3)

    def test_failed_pull_leaves_healthy_state_and_overlay_unchanged(self):
        state, overlay = self.state(), self.overlay()
        self.runner.fail_on = lambda a: 'pull' in a
        with self.assertRaises(module.DeploymentError):
            self.deploy()
        self.assertEqual(self.state(), state)
        self.assertEqual(self.overlay(), overlay)

    def test_failed_migration_does_not_restart_backend(self):
        old = self.overlay()
        self.runner.fail_on = lambda a: '--entrypoint' in a and 'up' in a
        with self.assertRaises(module.DeploymentError):
            self.deploy('backend')
        self.assertEqual(self.overlay(), old)
        self.assertEqual(self.state()['pending']['phase'], 'migration')
        self.assertFalse(any('--wait' in a for a, _ in self.runner.calls))

    def test_failed_health_does_not_mark_release_healthy_and_blocks_next_deploy(self):
        old = self.state()['services']['frontend']['image']
        self.health_failure = True
        with self.assertRaises(module.DeploymentError):
            self.deploy()
        self.assertEqual(self.state()['services']['frontend']['image'], old)
        self.assertTrue(self.state()['pending']['failed'])
        with self.assertRaises(module.DeploymentError):
            self.deploy('backend')

    def test_failed_rollback_retry_keeps_original_target_and_history(self):
        self.deploy(run=1, character='b')
        self.deploy(run=2, character='d')
        target = self.image('frontend', 'b')
        original = self.image('frontend', 'd')
        self.health_failure = True
        with self.assertRaises(module.DeploymentError):
            self.deployer.rollback('frontend')
        self.health_failure = False
        self.deployer.rollback('frontend')
        state = self.state()
        self.assertEqual(self.overlay()['services']['frontend']['image'], target)
        self.assertEqual(state['services']['frontend']['image'], target)
        self.assertEqual(state['services']['frontend']['previous']['image'], original)
        self.assertEqual(state['services']['frontend']['run_number'], 2)
        self.assertIsNone(state['pending'])

    def test_rollback_backend_requires_explicit_schema_confirmation(self):
        self.deploy('backend')
        self.runner.calls.clear()
        with self.assertRaises(module.DeploymentError):
            self.deployer.rollback('backend')
        self.assertEqual(self.runner.calls, [])

    def test_rollback_recovers_failed_candidate_and_preserves_high_water_mark(self):
        self.deploy(run=2)
        healthy = self.state()['services']['frontend']['image']
        self.health_failure = True
        with self.assertRaises(module.DeploymentError):
            self.deploy(run=3, character='d')
        self.health_failure = False
        self.deployer.rollback('frontend')
        self.assertEqual(self.overlay()['services']['frontend']['image'], healthy)
        self.assertIsNone(self.state()['pending'])
        self.runner.calls.clear()
        self.deploy(run=1)
        self.assertEqual(self.runner.calls, [])

    def test_rejects_image_from_other_registry_or_service_before_commands(self):
        for image in ('other.invalid/backend@sha256:' + 'b' * 64, self.image('backend')):
            with self.assertRaises(module.DeploymentError):
                self.deployer.deploy('frontend', image, 'c' * 40, 1, 'secret-token')
        self.assertEqual(self.runner.calls, [])

    def test_two_service_deployments_are_serialized_by_shared_vm_lock(self):
        entered, release, finished = threading.Event(), threading.Event(), threading.Event()
        errors = []
        other_runner = FakeDocker()

        def holding_health(service):
            entered.set()
            if not release.wait(3):
                raise module.DeploymentError('test release timeout')

        first = module.Deployer(self.cfg, self.runner, holding_health)
        second = module.Deployer(self.cfg, other_runner, self.health)

        def work(deployer, service):
            try:
                deployer.deploy(service, self.image(service), 'c' * 40, 1, 'secret-token')
            except Exception as error:
                errors.append(error)
            finally:
                if service == 'backend':
                    finished.set()

        a = threading.Thread(target=work, args=(first, 'frontend'))
        b = threading.Thread(target=work, args=(second, 'backend'))
        a.start()
        try:
            self.assertTrue(entered.wait(2))
            b.start()
            self.assertFalse(finished.wait(0.1))
            self.assertEqual(other_runner.calls, [])
        finally:
            release.set()
            a.join(4)
            if b.ident:
                b.join(4)
        self.assertFalse(a.is_alive() or b.is_alive())
        self.assertEqual(errors, [])
        for service in ('frontend', 'backend'):
            self.assertEqual(self.overlay()['services'][service]['image'], self.image(service))

    def test_registry_token_is_not_logged_and_docker_credentials_are_removed(self):
        self.deploy()
        login = next(a for a, _ in self.runner.calls if 'login' in a)
        temporary_directory = Path(login[login.index('--config') + 1])
        self.assertFalse(temporary_directory.exists())
        self.assertNotIn('secret-token', self.output.getvalue())
        self.assertFalse(any('secret-token' in arg for args, _ in self.runner.calls for arg in args))

    def test_command_failure_keeps_private_diagnostics_without_stdin_token(self):
        original = self.runner

        def failing(args, input=None):
            if 'pull' in args:
                raise module.CommandFailure(args, 1, '', 'permission denied fixture')
            return original(args, input=input)

        self.deployer.runner = failing
        with self.assertRaises(module.DeploymentError):
            self.deploy()
        error_file = self.root / 'state/last-error.json'
        self.assertIn('permission denied fixture', error_file.read_text())
        self.assertNotIn('secret-token', error_file.read_text())
        self.assertEqual(error_file.stat().st_mode & 0o777, 0o600)

    def test_wrong_hostname_fails_before_docker(self):
        cfg = dict(self.cfg, expected_hostname='wrong-vm')
        with self.assertRaises(module.DeploymentError):
            module.Deployer(cfg, self.runner, self.health).init()
        self.assertEqual(self.runner.calls, [])

    def test_invalid_commit_and_service_fail_before_commands(self):
        for service, commit in [('frontend', 'bad-sha'), ('mongodb', 'c' * 40)]:
            with self.assertRaises(module.DeploymentError):
                self.deployer.deploy(service, self.image(service), commit, 1, 'secret-token')
        self.assertEqual(self.runner.calls, [])

    def test_init_does_not_overwrite_existing_release(self):
        self.deploy()
        old = self.state()
        self.deployer.init()
        self.assertEqual(self.state(), old)


if __name__ == '__main__':
    unittest.main()
