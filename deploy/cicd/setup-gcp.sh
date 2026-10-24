#!/usr/bin/env bash
# Cloud Shell only. Outputs gcp-config.json containing no credentials.
set -euo pipefail
umask 077
if [[ $# -ne 3 ]]; then
  echo 'Usage: bash setup-gcp.sh GITHUB_OWNER_ID FRONTEND_REPOSITORY_ID BACKEND_REPOSITORY_ID' >&2
  exit 1
fi
for SS_ID in "$@"; do
  [[ "$SS_ID" =~ ^[1-9][0-9]*$ ]] || { echo 'GitHub IDs must be positive numeric IDs.' >&2; exit 1; }
done
[[ "$2" != "$3" ]] || { echo 'Frontend and backend repository IDs must differ.' >&2; exit 1; }
SS_PROJECT='project-795e1b7d-3b06-4c7b-bbe'
SS_VM='ss-demo'
SS_OWNER_ID="$1"
SS_FRONTEND_ID="$2"
SS_BACKEND_ID="$3"
SS_AR_REPOSITORY='ss-apps'
SS_POOL='ss-github'
SS_PROVIDER='github-main'
SS_SA="ss-cicd-deployer@$SS_PROJECT.iam.gserviceaccount.com"
SS_OPERATOR="$(gcloud auth list --filter=status:ACTIVE --format='value(account)')"
if [[ -z "$SS_OPERATOR" || "$SS_OPERATOR" == *$'\n'* || "$SS_OPERATOR" == *.gserviceaccount.com || "$SS_OPERATOR" != *@* ]]; then
  echo 'Use an authenticated human operator in Cloud Shell.' >&2
  exit 1
fi
SS_ZONE="$(gcloud compute instances list --project="$SS_PROJECT" --filter="name=$SS_VM" --format='value(zone.basename())')"
if [[ -z "$SS_ZONE" || "$SS_ZONE" == *$'\n'* ]]; then
  echo 'VM ss-demo not found or name is not unique; no cloud resources were changed.' >&2
  exit 1
fi
SS_REGION="${SS_ZONE%-*}"
SS_TMP="$(mktemp -d)"
trap 'rm -rf -- "$SS_TMP"' EXIT
# All lookups below precede IAM/metadata changes.
gcloud compute instances describe "$SS_VM" --project="$SS_PROJECT" --zone="$SS_ZONE" --format=json > "$SS_TMP/vm.json"
SS_VM_IP="$(python3 - "$SS_TMP/vm.json" <<'PY'
import ipaddress, json, sys
cfg=json.load(open(sys.argv[1]))
if cfg.get('status') != 'RUNNING': raise SystemExit('Application VM must be RUNNING.')
# x86 image workflows intentionally target linux/amd64.
if 'arm64' in cfg.get('cpuPlatform','').lower() or 'ampere' in cfg.get('cpuPlatform','').lower():
    raise SystemExit('ARM VM requires changing workflow platform before setup.')
print(ipaddress.IPv4Address(cfg['networkInterfaces'][0]['networkIP']))
PY
)"
SS_NETWORK_URL="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["networkInterfaces"][0]["network"])' "$SS_TMP/vm.json")"
[[ "$SS_NETWORK_URL" == *"/projects/$SS_PROJECT/"* ]] || { echo 'Shared VPC requires a network-admin-specific firewall setup.' >&2; exit 1; }
SS_NETWORK="${SS_NETWORK_URL##*/}"
SS_PROJECT_NUMBER="$(gcloud projects describe "$SS_PROJECT" --format='value(projectNumber)')"
[[ "$SS_PROJECT_NUMBER" =~ ^[0-9]+$ ]]
SS_WIF="projects/$SS_PROJECT_NUMBER/locations/global/workloadIdentityPools/$SS_POOL/providers/$SS_PROVIDER"

# A permission failure must not be interpreted as a missing resource.
ss_exists() {
  local SS_RESULT SS_CODE
  if "$@" > "$SS_TMP/describe.json" 2> "$SS_TMP/describe.err"; then return 0; else SS_CODE=$?; fi
  SS_RESULT="$(cat "$SS_TMP/describe.err")"
  case "$SS_RESULT" in
    *PERMISSION_DENIED*|*"Permission denied"*|*"permission denied"*|*"HTTPError 403"*)
      printf '%s\n' "$SS_RESULT" >&2; exit "$SS_CODE" ;;
    *NOT_FOUND*|*"HTTPError 404"*|*"was not found"*) return 1 ;;
    *) printf '%s\n' "$SS_RESULT" >&2; exit "$SS_CODE" ;;
  esac
}

gcloud services enable artifactregistry.googleapis.com iam.googleapis.com iamcredentials.googleapis.com \
  sts.googleapis.com compute.googleapis.com iap.googleapis.com oslogin.googleapis.com --project="$SS_PROJECT"
if ! ss_exists gcloud artifacts repositories describe "$SS_AR_REPOSITORY" --project="$SS_PROJECT" --location="$SS_REGION" --format=json; then
  gcloud artifacts repositories create "$SS_AR_REPOSITORY" --project="$SS_PROJECT" --location="$SS_REGION" --repository-format=docker
else
  python3 -c 'import json,sys; assert json.load(open(sys.argv[1]))["format"] == "DOCKER", "Existing registry is not Docker"' "$SS_TMP/describe.json"
fi
if ! ss_exists gcloud iam service-accounts describe "$SS_SA" --project="$SS_PROJECT" --format=json; then
  gcloud iam service-accounts create ss-cicd-deployer --project="$SS_PROJECT" --display-name='Standard Setting GitHub deployer'
fi
if ! ss_exists gcloud iam workload-identity-pools describe "$SS_POOL" --project="$SS_PROJECT" --location=global --format=json; then
  gcloud iam workload-identity-pools create "$SS_POOL" --project="$SS_PROJECT" --location=global --display-name='Standard Setting GitHub'
fi
SS_CONDITION="assertion.repository_owner_id == '$SS_OWNER_ID' && assertion.repository_id in ['$SS_FRONTEND_ID', '$SS_BACKEND_ID'] && assertion.ref == 'refs/heads/main' && assertion.event_name in ['push', 'workflow_dispatch']"
SS_MAPPING='google.subject=assertion.sub,attribute.repository_id=assertion.repository_id'
if ss_exists gcloud iam workload-identity-pools providers describe "$SS_PROVIDER" --project="$SS_PROJECT" --location=global --workload-identity-pool="$SS_POOL" --format=json; then
  gcloud iam workload-identity-pools providers update-oidc "$SS_PROVIDER" --project="$SS_PROJECT" --location=global \
    --workload-identity-pool="$SS_POOL" --issuer-uri=https://token.actions.githubusercontent.com \
    --attribute-mapping="$SS_MAPPING" --attribute-condition="$SS_CONDITION"
else
  gcloud iam workload-identity-pools providers create-oidc "$SS_PROVIDER" --project="$SS_PROJECT" --location=global \
    --workload-identity-pool="$SS_POOL" --issuer-uri=https://token.actions.githubusercontent.com \
    --attribute-mapping="$SS_MAPPING" --attribute-condition="$SS_CONDITION"
fi
for SS_REPOSITORY_ID in "$SS_FRONTEND_ID" "$SS_BACKEND_ID"; do
  gcloud iam service-accounts add-iam-policy-binding "$SS_SA" --project="$SS_PROJECT" \
    --role=roles/iam.workloadIdentityUser \
    --member="principalSet://iam.googleapis.com/projects/$SS_PROJECT_NUMBER/locations/global/workloadIdentityPools/$SS_POOL/attribute.repository_id/$SS_REPOSITORY_ID"
done
gcloud artifacts repositories add-iam-policy-binding "$SS_AR_REPOSITORY" --project="$SS_PROJECT" --location="$SS_REGION" \
  --member="serviceAccount:$SS_SA" --role=roles/artifactregistry.writer
gcloud projects add-iam-policy-binding "$SS_PROJECT" --member="serviceAccount:$SS_SA" --role=roles/compute.viewer --condition=None
SS_IAP_CONDITION="expression=destination.ip == '$SS_VM_IP' && destination.port == 22,title=ss-cicd-ssh-ss-demo"
for SS_MEMBER in "serviceAccount:$SS_SA" "user:$SS_OPERATOR"; do
  gcloud compute instances add-iam-policy-binding "$SS_VM" --project="$SS_PROJECT" --zone="$SS_ZONE" \
    --member="$SS_MEMBER" --role=roles/compute.osAdminLogin
  gcloud projects add-iam-policy-binding "$SS_PROJECT" --member="$SS_MEMBER" \
    --role=roles/iap.tunnelResourceAccessor --condition="$SS_IAP_CONDITION"
done
python3 -c 'import json,sys; [print(x["email"]) for x in json.load(open(sys.argv[1])).get("serviceAccounts",[])]' "$SS_TMP/vm.json" > "$SS_TMP/attached-accounts"
while IFS= read -r SS_ATTACHED_SA; do
  [[ -n "$SS_ATTACHED_SA" ]] || continue
  for SS_MEMBER in "serviceAccount:$SS_SA" "user:$SS_OPERATOR"; do
    gcloud iam service-accounts add-iam-policy-binding "$SS_ATTACHED_SA" --project="$SS_PROJECT" \
      --member="$SS_MEMBER" --role=roles/iam.serviceAccountUser
  done
done < "$SS_TMP/attached-accounts"
if ! ss_exists gcloud compute firewall-rules describe ss-cicd-iap-ssh --project="$SS_PROJECT" --format=json; then
  gcloud compute firewall-rules create ss-cicd-iap-ssh --project="$SS_PROJECT" --network="$SS_NETWORK" \
    --direction=INGRESS --priority=900 --action=ALLOW --rules=tcp:22 \
    --source-ranges=35.235.240.0/20 --target-tags=ss-cicd-ssh
else
  python3 - "$SS_TMP/describe.json" "$SS_NETWORK_URL" <<'PY'
import json, sys
r=json.load(open(sys.argv[1]))
assert r['network'] == sys.argv[2] and r.get('direction') == 'INGRESS' and not r.get('disabled',False), 'Unexpected firewall network/direction/state'
assert r.get('sourceRanges') == ['35.235.240.0/20'] and r.get('targetTags') == ['ss-cicd-ssh'], 'Unexpected firewall scope'
assert r.get('allowed') == [{'IPProtocol':'tcp','ports':['22']}], 'Unexpected firewall ports'
PY
fi
gcloud compute instances add-tags "$SS_VM" --project="$SS_PROJECT" --zone="$SS_ZONE" --tags=ss-cicd-ssh
# Operator OS Login rights are granted before enabling OS Login on this instance.
gcloud compute instances add-metadata "$SS_VM" --project="$SS_PROJECT" --zone="$SS_ZONE" --metadata=enable-oslogin=TRUE
python3 - "$SS_PROJECT" "$SS_VM" "$SS_ZONE" "$SS_REGION" "$SS_AR_REPOSITORY" "$SS_WIF" "$SS_SA" <<'PY'
import json, sys
from pathlib import Path
project,vm,zone,region,repo,wif,sa=sys.argv[1:]
variables={'GCP_PROJECT_ID':project,'GCP_VM_NAME':vm,'GCP_VM_ZONE':zone,
           'GCP_REGISTRY_LOCATION':region,'GCP_AR_REPOSITORY':repo,
           'GCP_WIF_PROVIDER':wif,'GCP_SERVICE_ACCOUNT':sa}
config={'variables':variables,'repositories':['BgsHryd/standard-setting-FE','BgsHryd/standard-setting-be'],
        'deploy_dir':'/home/bagushariyadi081/standard-setting/deploy',
        'state_dir':'/var/lib/standard-setting-cicd','expected_hostname':vm,
        'registry_prefix':f'{region}-docker.pkg.dev/{project}/{repo}'}
Path('gcp-config.json').write_text(json.dumps(config,indent=2)+'\n')
PY
echo 'Setup complete: gcp-config.json contains configuration, not credentials.'
echo 'Keep your existing SSH session open. Test a SECOND operator SSH session through IAP before closing it.'
echo 'IAM/WIF propagation can take several minutes. Install VM tooling before publishing workflows.'
