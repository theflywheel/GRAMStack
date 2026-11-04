#!/usr/bin/env bash
# Produce an allowlisted upload directory from a committed source tree.
set -euo pipefail
root=$(cd "$(dirname "$0")/../.." && pwd)
cd "$root"
service=${1:?Pass a Railway service name}
revision=$(git rev-parse --verify "${2:-HEAD}^{commit}")
case "$service" in
  docs) paths=(docs specs .dockerignore .railwayignore) ;;
  platform-db) paths=(infra/railway/database) ;;
  gateway|lokos-adapter|mock-lokos|sakhicredit)
    paths=(go.work services/gateway sdk/go/adapter adapters/auth/oidc adapters/lokos
      mocks/identity mocks/lokos apps/sakhicredit tools/runtimefiles/main.go
      .dockerignore .railwayignore)
    ;;
  *) echo "Unknown deployment service: $service" >&2; exit 1 ;;
esac
manifest="infra/railway/deploy/$service.json"
stage=$(mktemp -d /tmp/gramstack-railway-source.XXXXXX)
trap 'rm -rf "$stage"' EXIT
git archive "$revision" "${paths[@]}" "$manifest" | tar -xf - -C "$stage"
mv "$stage/$manifest" "$stage/railway.json"
if [[ -n $(find "$stage" -type l -print -quit) ]]; then
  echo 'Deployment source contains a symbolic link' >&2
  exit 1
fi
trap - EXIT
printf '%s\n' "$stage"
