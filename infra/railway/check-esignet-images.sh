#!/usr/bin/env bash
# Verify the exact committed eSignet artifacts on a fresh local issuer.
set -euo pipefail
root=$(cd "$(dirname "$0")/../.." && pwd)
cd "$root"
revision=$(git rev-parse --verify "${1:-HEAD}^{commit}")
: "${PG_BIN:?Set PG_BIN for the live gateway and browser test}"
: "${HYDRA_BIN:?Set HYDRA_BIN to the pinned official Hydra executable}"
umask 077
state=$(mktemp -d /tmp/gramstack-esignet-images.XXXXXX)
stage=''
compose=(docker compose -f infra/esignet/compose.yaml -f infra/esignet/ingress-test.compose.yaml)
cleanup() {
  status=$?
  if [[ -n "${COMPOSE_PROJECT_NAME:-}" ]]; then
    if [[ "$status" != 0 ]]; then "${compose[@]}" logs --tail 30 database >&2 || true; fi
    "${compose[@]}" down --volumes >/dev/null 2>&1 || true
  fi
  if [[ -n "$stage" ]]; then rm -rf "$stage"; fi
  rm -rf "$state"
}
unset COMPOSE_PROJECT_NAME
trap cleanup EXIT
for service in esignet-db esignet-mock esignet esignet-ui esignet-redis; do
  image="gramstack-$service:deployment-check"
  if [[ "$service" == esignet-ui ]]; then image=gramstack-esignet-ingress:deployment-check; fi
  stage=$(./infra/railway/stage.sh "$service" "$revision")
  docker build --progress=plain -f "$stage/infra/railway/$service/Dockerfile" -t "$image" "$stage"
  rm -rf "$stage"
  stage=''
done

python3 infra/esignet/init-demo.py "$state/esignet"
set -a
source "$state/esignet/.env"
set +a
export ESIGNET_DEMO_ISSUER_PORT="$ESIGNET_DEMO_UI_PORT"
"${compose[@]}" up -d --no-build --wait --wait-timeout 240
python3 infra/esignet/check.py
public="http://127.0.0.1:$ESIGNET_DEMO_UI_PORT"
for path in /client-mgmt/client /v1/esignet/client-mgmt/oidc-client /oauth2/par /actuator/health; do
  [[ $(curl --disable -sS --max-time 5 -o /dev/null -w '%{http_code}' "$public$path") == 404 ]]
done
# Recreate the API and database containers on the original persistent volumes.
"${compose[@]}" up -d --no-build --force-recreate --wait --wait-timeout 240 database esignet
python3 infra/esignet/check.py
export GRAMSTACK_TEST_ESIGNET=1 GRAMSTACK_TEST_PRODUCT_UI=1
if [[ "${GRAMSTACK_TEST_ANCHORING:-}" == 1 ]]; then
  ./scripts/test-dedi.sh
else
  ./scripts/test-hydra.sh
fi
