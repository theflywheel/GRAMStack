#!/usr/bin/env bash
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT
cmp "$root/specs/0.1.0/lifecycle.json" "$root/services/gateway/internal/access/testdata/lifecycle.json"
for module in sdk/go/adapter services/gateway; do
  GOWORK=off go -C "$root/$module" test -race ./...
  GOWORK=off go -C "$root/$module" vet ./...
done
# Before the first SDK release, resolve its source explicitly in a temporary
# module file. No adapter go.mod replace or implicit Go workspace is required.
cp "$root/adapters/lokos/go.mod" "$scratch/lokos.mod"
GOWORK=off go -C "$root/adapters/lokos" mod edit -modfile="$scratch/lokos.mod" -replace="github.com/theflywheel/GRAMStack/sdk/go/adapter=$root/sdk/go/adapter"
GOWORK=off go -C "$root/adapters/lokos" test -modfile="$scratch/lokos.mod" -race ./...
GOWORK=off go -C "$root/adapters/lokos" vet -modfile="$scratch/lokos.mod" ./...
GOWORK=off go -C "$root/services/gateway" build -o "$scratch/gateway" ./cmd/gramstack
GOWORK=off go -C "$root/adapters/lokos" build -modfile="$scratch/lokos.mod" -o "$scratch/lokos-adapter" ./cmd/lokos-adapter

for executable in gateway lokos-adapter; do
  port=$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()')
  PORT="$port" "$scratch/$executable" > "$scratch/$executable.log" 2>&1 &
  pid=$!
  trap 'kill "$pid" 2>/dev/null || true; rm -rf "$scratch"' EXIT
  ready=false
  for _ in $(seq 1 100); do
    if curl --fail --silent "http://127.0.0.1:$port/healthz" >/dev/null; then ready=true; break; fi
    sleep 0.1
  done
  if [[ "$ready" != true ]]; then echo "$executable failed liveness" >&2; exit 1; fi
  [[ $(curl --silent -o /dev/null -w '%{http_code}' "http://127.0.0.1:$port/readyz") == 503 ]]
  [[ $(curl --silent -o /dev/null -w '%{http_code}' "http://127.0.0.1:$port/internal/v1/retrievals") == 404 ]]
  kill -TERM "$pid"
  wait "$pid"
  trap 'rm -rf "$scratch"' EXIT
  echo "$executable process smoke test passed"
done
