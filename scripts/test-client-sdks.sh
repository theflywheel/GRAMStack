#!/usr/bin/env bash
# One isolated TLS/OIDC fixture verifies the same consumer behavior across languages.
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
: "${SDK_PYTHON:?Set SDK_PYTHON to a Python >=3.10 environment with the Python SDK and pytest installed}"
umask 077
scratch=$(mktemp -d /tmp/gramstack-sdks.XXXXXX)
pid=''
cleanup(){
 if [[ -n "$pid" ]]; then kill "$pid" 2>/dev/null || true; wait "$pid" 2>/dev/null || true; fi
 rm -rf "$scratch"
}
trap cleanup EXIT
"$SDK_PYTHON" "$root/sdk/conformance/server.py" "$scratch/config.json" >"$scratch/server.log" 2>&1 &
pid=$!
for _ in $(seq 1 60); do
 if [[ -s "$scratch/config.json" ]]; then break; fi
 if ! kill -0 "$pid" 2>/dev/null; then cat "$scratch/server.log" >&2; exit 1; fi
 sleep .1
done
test -s "$scratch/config.json"
export GRAMSTACK_SDK_FIXTURE="$scratch/config.json"
"$SDK_PYTHON" -m pytest "$root/sdk/python/tests" -q
node --test "$root"/sdk/javascript/tests/*.test.mjs
if [[ "${GRAMSTACK_SKIP_GO_SDK:-}" != 1 ]]; then
 GOWORK=off go -C "$root/sdk/go/client" test -race ./...
fi
