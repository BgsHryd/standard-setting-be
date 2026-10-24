import os
from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]


class SetupValidation(unittest.TestCase):
    def run_setup(self, ids, cli):
        with tempfile.TemporaryDirectory() as d:
            folder = Path(d)
            tool = folder / 'gcloud'
            tool.write_text('#!/usr/bin/env bash\n' + cli)
            tool.chmod(0o755)
            result = subprocess.run(['bash', str(ROOT / 'setup-gcp.sh'), *ids],
                                    env={**os.environ, 'PATH': str(folder) + ':' + os.environ['PATH']},
                                    text=True, capture_output=True, cwd=folder)
            return result

    def test_invalid_repository_ids_fail_before_cloud_commands(self):
        r = self.run_setup(['owner', '123', '456'], 'echo CLOUD_COMMAND >&2; exit 9\n')
        self.assertNotEqual(r.returncode, 0)
        self.assertIn('numeric', r.stderr)
        self.assertNotIn('CLOUD_COMMAND', r.stderr)

    def test_missing_vm_does_not_create_resources(self):
        cli = '''
if [[ "$*" == *"auth list"* ]]; then echo operator@example.com; exit 0; fi
if [[ "$*" == *"instances list"* ]]; then exit 0; fi
echo UNEXPECTED_MUTATION >&2; exit 9
'''
        r = self.run_setup(['123', '456', '789'], cli)
        self.assertNotEqual(r.returncode, 0)
        self.assertIn('VM', r.stderr)
        self.assertNotIn('UNEXPECTED_MUTATION', r.stderr)

    def test_denied_lookup_stops_without_creating_resources(self):
        cli = '''
if [[ "$*" == *"auth list"* ]]; then echo operator@example.com; exit 0; fi
if [[ "$*" == *"instances list"* ]]; then echo PERMISSION_DENIED >&2; exit 7; fi
echo UNEXPECTED_MUTATION >&2; exit 9
'''
        r = self.run_setup(['123', '456', '789'], cli)
        self.assertEqual(r.returncode, 7)
        self.assertNotIn('UNEXPECTED_MUTATION', r.stderr)

    def test_denied_registry_describe_is_not_treated_as_missing_registry(self):
        cli = r'''
case "$*" in
  *"auth list"*) echo operator@example.com ;;
  *"instances list"*) echo asia-southeast2-a ;;
  *"instances describe"*) echo '{"status":"RUNNING","cpuPlatform":"Intel Broadwell","networkInterfaces":[{"networkIP":"10.1.0.2","network":"https://www.googleapis.com/compute/v1/projects/project-795e1b7d-3b06-4c7b-bbe/global/networks/default"}]}' ;;
  *"projects describe"*) echo 123456 ;;
  *"services enable"*) exit 0 ;;
  *"artifacts repositories describe"*) echo PERMISSION_DENIED >&2; exit 7 ;;
  *) echo UNEXPECTED_CREATE >&2; exit 9 ;;
esac
'''
        r = self.run_setup(['123', '456', '789'], cli)
        self.assertEqual(r.returncode, 7)
        self.assertNotIn('UNEXPECTED_CREATE', r.stderr)

    def test_configure_github_rejects_wrong_repository_mapping(self):
        with tempfile.TemporaryDirectory() as d:
            cfg = Path(d) / 'config.json'
            cfg.write_text('{"repositories": ["attacker/other"]}')
            r = subprocess.run(['python3', str(ROOT / 'configure-github.py'), str(cfg)],
                               text=True, capture_output=True)
            self.assertNotEqual(r.returncode, 0)
            self.assertIn('invalid repository mapping', r.stderr.lower())


if __name__ == '__main__':
    unittest.main()
