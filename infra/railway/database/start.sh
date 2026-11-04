#!/usr/bin/env bash
set -euo pipefail

# Validate configuration before the upstream entrypoint can initialize PGDATA.
if [[ "${1:-}" == -* ]]; then set -- postgres "$@"; fi
if [[ "${1:-}" == postgres && ! -s "$PGDATA/PG_VERSION" ]]; then
  for arg in "$@"; do
    case "$arg" in
      -? | --help | --describe-config | -V | --version)
        exec /usr/local/bin/docker-entrypoint.sh "$@"
        ;;
    esac
  done
  for role in CORE ADAPTER PRODUCT HYDRA DEDI; do
    key="GRAMSTACK_DB_${role}_PASSWORD"
    if [[ -z "${!key:-}" ]]; then
      echo "Required database role password is missing: $key" >&2
      exit 1
    fi
  done
fi

exec /usr/local/bin/docker-entrypoint.sh "$@"
