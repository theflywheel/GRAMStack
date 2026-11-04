#!/usr/bin/env bash
# Produce an allowlisted upload directory from a committed source tree.
set -euo pipefail
root=$(cd "$(dirname "$0")/../.." && pwd)
cd "$root"
service=${1:?Pass a Railway service name}
revision=$(git rev-parse --verify "${2:-HEAD}^{commit}")
case "$service" in
  docs) paths=(docs specs .dockerignore .railwayignore) ;;
  platform-db) paths=(infra/railway/database infra/esignet/init.sql) ;;
  dedi) paths=(infra/railway/dedi tools/runtimefiles/main.go) ;;
  esignet) paths=(infra/railway/esignet infra/esignet/flow-local-demo.yaml) ;;
  esignet-mock) paths=(infra/railway/esignet-mock infra/railway/esignet/keystore-start.sh) ;;
  esignet-ui) paths=(infra/railway/esignet-ui) ;;
  esignet-redis) paths=(infra/railway/esignet-redis) ;;
  gateway|lokos-adapter|mock-lokos|sakhicredit)
    paths=(go.work services/gateway sdk/go/adapter adapters/auth/oidc adapters/lokos
      mocks/identity mocks/lokos apps/sakhicredit tools/runtimefiles/main.go
      .dockerignore .railwayignore)
    ;;
  *) echo "Unknown deployment service: $service" >&2; exit 1 ;;
esac
stage=$(mktemp -d /tmp/gramstack-railway-source.XXXXXX)
trap 'rm -rf "$stage"' EXIT
# Keep source modes consistent even when the operator uses a private umask.
# mktemp still keeps the upload directory itself accessible only to its owner.
git archive "$revision" "${paths[@]}" | (umask 022; tar -xf - -C "$stage")
if [[ -n $(find "$stage" -type l -print -quit) ]]; then
  echo 'Deployment source contains a symbolic link' >&2
  exit 1
fi
trap - EXIT
printf '%s\n' "$stage"
