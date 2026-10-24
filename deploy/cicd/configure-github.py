#!/usr/bin/env python3
"""Set non-secret repository variables using an already authenticated gh CLI."""
import json
from pathlib import Path
import re
import subprocess
import sys

REPOSITORIES = ['BgsHryd/standard-setting-FE', 'BgsHryd/standard-setting-be']
VARIABLES = {'GCP_PROJECT_ID', 'GCP_VM_NAME', 'GCP_VM_ZONE', 'GCP_REGISTRY_LOCATION',
             'GCP_AR_REPOSITORY', 'GCP_WIF_PROVIDER', 'GCP_SERVICE_ACCOUNT'}


def main():
    if len(sys.argv) != 2:
        raise SystemExit('Usage: python3 configure-github.py GCP_CONFIG_JSON')
    cfg = json.loads(Path(sys.argv[1]).read_text())
    if cfg.get('repositories') != REPOSITORIES:
        raise SystemExit('Invalid repository mapping; expected the two Standard Setting repositories.')
    if set(cfg.get('variables', {})) != VARIABLES:
        raise SystemExit('Invalid GitHub variable configuration')
    for name, value in cfg['variables'].items():
        if not isinstance(value, str) or not value or not re.fullmatch(r'[a-zA-Z0-9@._/-]+', value):
            raise SystemExit('Invalid variable: ' + name)
    subprocess.run(['gh', 'auth', 'status'], check=True)
    for repo in REPOSITORIES:
        for name, value in cfg['variables'].items():
            subprocess.run(['gh', 'variable', 'set', name, '--repo', repo, '--body', value], check=True)
        print('Configured repository variables: ' + repo)


if __name__ == '__main__':
    main()
