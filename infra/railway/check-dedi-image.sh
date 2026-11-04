#!/usr/bin/env bash
# Run from check-database.sh against its isolated fixture network.
set -euo pipefail
root=$(cd "$(dirname "$0")/../.." && pwd)
state=${1:?fixture directory required}
network=${2:?fixture network required}
revision=${3:?committed revision required}
case "$network" in gramstack-railway-db.*) ;; *) exit 1 ;; esac
[[ "$state" == "/tmp/$network" && -f "$state/dedi-db.env" ]]
umask 077
container="$network-dedi"
stage=$("$root/infra/railway/stage.sh" dedi "$revision")
cleanup() {
  docker rm -f "$container" >/dev/null 2>&1 || true
  rm -rf "$stage"
}
trap cleanup EXIT
image=gramstack-dedi:deployment-check
docker build --progress=plain -f "$stage/infra/railway/dedi/Dockerfile" -t "$image" "$stage"
docker run --rm --network none --entrypoint /bin/sh "$image" -ec '
  /app/dedid keygen -out /run/gramstack/node.key -name railway-test >&2
  cat /run/gramstack/node.key
' >"$state/dedi-node.key" 2>"$state/dedi-node-public.txt"
docker run --rm --network none --entrypoint /bin/sh "$image" -ec '
  /app/dedid pubkeygen -out /run/gramstack/publisher.key -kid railway-test -namespace railway-test >&2
  cat /run/gramstack/publisher.key
' >"$state/dedi-publisher.key" 2>"$state/dedi-publisher-public.txt"
cat >"$state/dedi-cert.cnf" <<'CERT'
[req]
prompt=no
distinguished_name=subject
x509_extensions=extensions
[subject]
CN=dedi.railway.internal
[extensions]
subjectAltName=DNS:dedi.railway.internal
basicConstraints=critical,CA:TRUE
CERT
openssl req -x509 -newkey rsa:2048 -nodes -days 1 -config "$state/dedi-cert.cnf" \
  -keyout "$state/dedi.key" -out "$state/dedi.pem" 2>/dev/null

python3 - "$state" <<'PY'
import base64
import json
from pathlib import Path
import secrets
import sys

root = Path(sys.argv[1])
def bundle(files):
    return "GRAMSTACK_FILES_JSON=" + json.dumps({
        name: base64.b64encode((root / path).read_bytes()).decode() for name, path in files.items()
    }, separators=(",", ":"))

password = secrets.token_urlsafe(24)
publisher = next(line for line in (root / "dedi-publisher-public.txt").read_text().splitlines()
                 if line.startswith("DEDI_PUBLISHER_KEYS="))
checkpoint = (root / "dedi-node-public.txt").read_text().splitlines()[-1]
(root / "dedi.env").write_text((root / "dedi-db.env").read_text() + "\n".join((
    "DEDI_ADMIN_PASSWORD=" + password, publisher, "DEDI_ORIGIN=railway-test/log",
    "DEDI_KEY_FILE=/run/gramstack/node.key",
    bundle({"node.key": "dedi-node.key", "dedi.pem": "dedi.pem", "dedi.key": "dedi.key"}),
)) + "\n")
(root / "dedi-gateway.env").write_text("\n".join((
    "DEDI_ADMIN_PASSWORD=" + password, "DEDI_ADMIN_USER=admin",
    "DEDI_NODE_URL=https://dedi.railway.internal:8446", "DEDI_NAMESPACE=railway-test",
    "DEDI_REGISTRY=commitments", "DEDI_PUBLISHER_KEY_ID=railway-test",
    "DEDI_CHECKPOINT_ORIGIN=railway-test/log", "DEDI_CHECKPOINT_VERIFIER=" + checkpoint,
    "DEDI_PUBLISHER_KEY_FILE=/run/gramstack/publisher.key",
    "GRAMSTACK_PRIVATE_CA_FILE=/run/gramstack/dedi-ca.pem",
    bundle({"publisher.key": "dedi-publisher.key", "dedi-ca.pem": "dedi.pem"}),
)) + "\n")
PY

docker run -d --name "$container" --network "$network" --network-alias dedi.railway.internal \
  --env-file "$state/dedi.env" "$image" >/dev/null
probe() {
  docker exec "$network" timeout 4 bash -ec '
    exec 3<>/dev/tcp/dedi.railway.internal/8080
    printf "GET %s HTTP/1.0\r\nHost: dedi.railway.internal\r\n\r\n" "$1" >&3
    IFS=" " read -r _ status _ <&3
    printf "%s\n" "$status"
  ' _ "$1"
}
wait_ready() {
  for _ in $(seq 1 60); do
    if [[ $(probe /healthz 2>/dev/null || true) == 200 ]]; then return; fi
    if [[ $(docker inspect --format '{{.State.Running}}' "$container") != true ]]; then break; fi
    sleep 0.2
  done
  echo 'Staged DeDi did not reach private readiness.' >&2
  return 1
}
anchor() {
  docker run --rm --network "$network" --env-file "$state/dedi-gateway.env" \
    gramstack-gateway:deployment-check /app/start-gateway anchor "$@"
}
wait_ready
[[ $(probe /admin/namespaces) == 404 ]]
anchor -provision
digest="sha256:$(printf '%064d' 0)"
anchor -event-id railway-fixture -digest "$digest" >"$state/dedi-proof-before.json"
docker restart "$container" >/dev/null
wait_ready
anchor -event-id railway-fixture -digest "$digest" >"$state/dedi-proof-after.json"
python3 - "$state" <<'PY'
import json
from pathlib import Path
import sys

root = Path(sys.argv[1])
before, after = (json.loads((root / f"dedi-proof-{phase}.json").read_text())
                 for phase in ("before", "after"))
assert before["record_id"] == after["record_id"]
assert before["tree_size"] == after["tree_size"] > 0
assert before["leaf_index"] == after["leaf_index"]
print("PASS: staged DeDi TLS, health-only probe, and verified commitment recovery after restart")
PY
