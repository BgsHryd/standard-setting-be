#!/usr/bin/env bash
# Run on ss-demo: sudo bash install-vm.sh /path/to/gcp-config.json
set -euo pipefail
[[ $EUID -eq 0 ]] || { echo 'Run with sudo on the application VM.' >&2; exit 1; }
[[ $# -eq 1 ]] || { echo 'Usage: install-vm.sh GCP_CONFIG_JSON' >&2; exit 1; }
SS_SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
command -v python3 >/dev/null
command -v docker >/dev/null
docker compose version >/dev/null
python3 - "$1" <<'PY'
import json, socket, sys
from pathlib import Path
cfg = json.loads(Path(sys.argv[1]).read_text())
if socket.gethostname().split('.')[0] != cfg['expected_hostname']:
    raise SystemExit('Wrong VM; install only on configured application VM.')
if cfg['state_dir'] != '/var/lib/standard-setting-cicd':
    raise SystemExit('Unexpected state directory')
for name in ('compose.yaml', 'compose.remote-data.yaml', '.env.remote'):
    if not (Path(cfg['deploy_dir']) / name).is_file():
        raise SystemExit('Missing existing deployment file: ' + name)
existing = Path('/etc/standard-setting-cicd/config.json')
if existing.exists():
    previous = json.loads(existing.read_text())
    for key in ('deploy_dir', 'state_dir', 'registry_prefix', 'expected_hostname'):
        if previous[key] != cfg[key]:
            raise SystemExit('Configuration changed; reconcile existing state before reinstalling: ' + key)
PY
install -d -m 700 /etc/standard-setting-cicd /var/lib/standard-setting-cicd
python3 - "$1" <<'PY'
import json, os, sys, tempfile
from pathlib import Path
cfg = json.loads(Path(sys.argv[1]).read_text())
keys = ('deploy_dir', 'state_dir', 'registry_prefix', 'expected_hostname')
fd, tmp = tempfile.mkstemp(dir='/etc/standard-setting-cicd')
with os.fdopen(fd, 'w') as f:
    json.dump({key: cfg[key] for key in keys}, f, indent=2)
    f.write('\n')
os.replace(tmp, '/etc/standard-setting-cicd/config.json')
PY
install -m 755 "$SS_SCRIPT_DIR/deploy.py" /usr/local/sbin/ss-deploy
/usr/local/sbin/ss-deploy init
/usr/local/sbin/ss-deploy status
echo 'VM tooling installed. Verify operator OS Login and GitHub configuration before enabling workflows.'
