#!/usr/bin/env bash
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
scratch=$(mktemp -d)
pids=()
logs=()
smoke_pid=''
cleanup(){
  if [[ -n "$smoke_pid" ]]; then kill "$smoke_pid" 2>/dev/null || true; wait "$smoke_pid" 2>/dev/null || true; fi
  for pid in "${pids[@]:-}"; do if [[ -n "$pid" ]]; then kill "$pid" 2>/dev/null || true; wait "$pid" 2>/dev/null || true; fi; done
  rm -rf "$scratch"
}
trap cleanup EXIT
cmp "$root/specs/0.2.0/lifecycle.json" "$root/services/gateway/internal/access/testdata/lifecycle.json"
# Before the first SDK release, resolve its source in a temporary module file.
cp "$root/adapters/lokos/go.mod" "$scratch/lokos.mod"
GOWORK=off go -C "$root/adapters/lokos" mod edit -modfile="$scratch/lokos.mod" -replace="github.com/theflywheel/GRAMStack/sdk/go/adapter=$root/sdk/go/adapter"
check_module(){
  local module=$1
  shift
  GOWORK=off go -C "$root/$module" test -race "$@" ./...
  GOWORK=off go -C "$root/$module" vet "$@" ./...
}
launch(){
  local name=$1
  shift
  "$@" > "$scratch/$name.log" 2>&1 &
  pids+=("$!")
  logs+=("$scratch/$name.log")
}
wait_checks(){
  local failed=0 index
  for index in "${!pids[@]}"; do
    if wait "${pids[$index]}"; then cat "${logs[$index]}"; else cat "${logs[$index]}" >&2; failed=1; fi
  done
  pids=()
  logs=()
  return "$failed"
}
# Two workers bound compiler/memory contention on small CI runners. Logs stay
# per module and every worker's exit status is collected before the next gate.
launch gateway check_module services/gateway
launch sdk check_module sdk/go/adapter
wait_checks
launch lokos check_module adapters/lokos -modfile="$scratch/lokos.mod"
launch identity check_module mocks/identity
wait_checks
launch gateway-build env GOWORK=off go -C "$root/services/gateway" build -o "$scratch/gateway" ./cmd/gramstack
launch lokos-build env GOWORK=off go -C "$root/adapters/lokos" build -modfile="$scratch/lokos.mod" -o "$scratch/lokos-adapter" ./cmd/lokos-adapter
wait_checks

for executable in gateway lokos-adapter; do
  port=$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()')
  PORT="$port" "$scratch/$executable" > "$scratch/$executable.log" 2>&1 &
  pid=$!
  smoke_pid=$pid
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
  smoke_pid=''
  echo "$executable process smoke test passed"
done
