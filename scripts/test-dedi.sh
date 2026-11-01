#!/usr/bin/env bash
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
: "${DEDI_SOURCE:?Set DEDI_SOURCE to an existing DeDi-node checkout}"
: "${PG_BIN:?Set PG_BIN to the PostgreSQL bin directory containing initdb and pg_ctl}"
scratch=$(mktemp -d /tmp/gramstack-dedi.XXXXXX)
node_pid=""
cleanup() {
  if [[ -n "$node_pid" ]]; then kill "$node_pid" 2>/dev/null || true; wait "$node_pid" 2>/dev/null || true; fi
  "$PG_BIN/pg_ctl" -D "$scratch/pg" -m immediate -w stop >/dev/null 2>&1 || true
  rm -rf "$scratch"
}
trap cleanup EXIT
if [[ -n "${DEDI_BINARY:-}" ]]; then cp "$DEDI_BINARY" "$scratch/dedid"; else
  GOWORK=off go -C "$DEDI_SOURCE" build -mod=readonly -o "$scratch/dedid" ./cmd/dedid
fi
"$PG_BIN/initdb" -D "$scratch/pg" -U gramstack --auth-local=trust --auth-host=scram-sha-256 > "$scratch/initdb.log"
"$PG_BIN/pg_ctl" -D "$scratch/pg" -l "$scratch/pg.log" -o "-k $scratch -p 55439 -c listen_addresses=''" -w start >/dev/null
"$PG_BIN/createdb" -h "$scratch" -p 55439 -U gramstack gramstack_dedi
"$scratch/dedid" keygen -out "$scratch/node.key" -name gramstack-test-node > "$scratch/node-public.txt"
keys=$("$scratch/dedid" pubkeygen -out "$scratch/publisher.key" -kid gramstack-test -namespace gramstack-test | sed -n 's/^DEDI_PUBLISHER_KEYS=//p')
port=$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()')
password=$(python3 -c 'import secrets; print(secrets.token_urlsafe(24))')
DEDI_DB_URL="postgres://gramstack@localhost/gramstack_dedi?host=$scratch&port=55439&sslmode=disable" \
DEDI_KEY_FILE="$scratch/node.key" DEDI_ORIGIN=gramstack-test/log \
DEDI_PUBLISHER_KEYS="$keys" DEDI_ADMIN_PASSWORD="$password" DEDI_LISTEN="127.0.0.1:$port" \
"$scratch/dedid" serve > "$scratch/node.log" 2>&1 &
node_pid=$!
ready=false
for _ in $(seq 1 100); do
  if curl --fail --silent "http://127.0.0.1:$port/healthz" >/dev/null; then ready=true; break; fi
  if ! kill -0 "$node_pid" 2>/dev/null; then echo "DeDi exited before readiness" >&2; exit 1; fi
  sleep 0.1
done
if [[ "$ready" != true ]]; then echo "DeDi failed readiness" >&2; exit 1; fi
export GRAMSTACK_TEST_DEDI_URL="http://127.0.0.1:$port"
export GRAMSTACK_TEST_DEDI_PUBLISHER_KEY=$(cat "$scratch/publisher.key")
export GRAMSTACK_TEST_DEDI_PASSWORD="$password"
export GRAMSTACK_TEST_DEDI_VERIFIER=$(tail -n 1 "$scratch/node-public.txt")
GOWORK=off go -C "$root/services/gateway" test -race -count=1 -v ./internal/evidence/dedi -run TestLiveNode
export DEDI_NODE_URL="$GRAMSTACK_TEST_DEDI_URL" DEDI_NAMESPACE=gramstack-test DEDI_REGISTRY=commitments
export DEDI_PUBLISHER_KEY_ID=gramstack-test DEDI_PUBLISHER_KEY_FILE="$scratch/publisher.key"
export DEDI_ADMIN_USER=admin DEDI_ADMIN_PASSWORD="$password" DEDI_CHECKPOINT_ORIGIN=gramstack-test/log
export DEDI_CHECKPOINT_VERIFIER="$GRAMSTACK_TEST_DEDI_VERIFIER"
GOWORK=off go -C "$root/services/gateway" run ./cmd/gramstack anchor -development-loopback -event-id cli-integration-event -digest "sha256:$(printf '%064d' 0)" > "$scratch/anchor.json"
python3 - "$scratch/anchor.json" <<'CHECK'
import json,sys
v=json.load(open(sys.argv[1]));assert v['tree_size']>0 and len(v['record_id'])==64 and v['checkpoint']
print('Gateway CLI verified a real DeDi anchor and inclusion proof.')
CHECK
