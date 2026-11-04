#!/usr/bin/env bash
# Build staged Railway source and probe only containers owned by this run.
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
scratch=$(mktemp -d)
container=''
stage=''
revision=$(git -C "$root" rev-parse --verify "${1:-HEAD}^{commit}")
cleanup(){
  if [[ -n "$container" ]]; then docker rm -f "$container" >/dev/null 2>&1 || true; fi
  if [[ -n "$stage" ]]; then rm -rf "$stage"; fi
  rm -rf "$scratch"
}
trap cleanup EXIT
for role in gateway adapter source product; do
  case "$role" in
    gateway) directory=services/gateway; railway_service=gateway ;;
    adapter) directory=adapters/lokos; railway_service=lokos-adapter ;;
    source) directory=mocks/lokos; railway_service=mock-lokos ;;
    product) directory=apps/sakhicredit; railway_service=sakhicredit ;;
  esac
  image="gramstack-$role:deployment-check"
  stage=$("$root/infra/railway/stage.sh" "$railway_service" "$revision")
  docker build --progress=plain -f "$stage/$directory/Dockerfile" -t "$image" "$stage"
  rm -rf "$stage"
  stage=''
  docker run --rm -e 'GRAMSTACK_FILES_JSON={"probe.txt":"cHJvYmU="}' "$image" /bin/sh -ec '
    test "$(id -u)" = 10001
    test -z "${GRAMSTACK_FILES_JSON+x}"
    test "$(stat -c %a /run/gramstack)" = 700
    test "$(stat -c %a /run/gramstack/probe.txt)" = 600
    test "$(cat /run/gramstack/probe.txt)" = probe
    test ! -e /usr/local/go/bin/go
    test ! -e /usr/bin/node
  '
  if [[ "$role" == source ]]; then
    if docker run --rm "$image" > "$scratch/source.log" 2>&1; then
      echo 'Fictional source started without explicit demo configuration' >&2; exit 1
    fi
    grep -q 'fictional source requires development/demo mode' "$scratch/source.log"
    continue
  fi
  container=$(docker run --detach --publish 127.0.0.1::8080 "$image")
  port=$(docker port "$container" 8080/tcp)
  ready=false
  for _ in $(seq 1 50); do
    if curl --fail --silent "http://$port/healthz" >/dev/null; then ready=true; break; fi
    sleep 0.1
  done
  if [[ "$ready" != true ]]; then docker logs "$container" >&2; exit 1; fi
  [[ $(curl --silent -o /dev/null -w '%{http_code}' "http://$port/readyz") == 503 ]]
  docker stop --time 15 "$container" >/dev/null
  [[ $(docker inspect --format '{{.State.ExitCode}}' "$container") == 0 ]]
  docker rm "$container" >/dev/null
  container=''
  echo "$role image: non-root, private files, fail-closed readiness and graceful shutdown passed"
done
echo 'All four deployment images passed.'
