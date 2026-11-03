#!/usr/bin/env bash
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
: "${PG_BIN:?Set PG_BIN to the PostgreSQL binary directory}"
scratch=$(mktemp -d /tmp/gramstack-sql.XXXXXX)
cleanup(){
  "$PG_BIN/pg_ctl" -D "$scratch/pg" -m immediate -w stop >/dev/null 2>&1 || true
  rm -rf "$scratch"
}
trap cleanup EXIT
source "$root/scripts/gateway-module.sh"
prepare_gateway_module "$root" "$scratch"
"$PG_BIN/initdb" -D "$scratch/pg" -U gramstack --auth-local=trust --auth-host=scram-sha-256 > "$scratch/initdb.log"
"$PG_BIN/pg_ctl" -D "$scratch/pg" -l "$scratch/pg.log" -o "-k $scratch -p 55439 -c listen_addresses=''" -w start >/dev/null
"$PG_BIN/createdb" -h "$scratch" -p 55439 -U gramstack gramstack_core
export GRAMSTACK_TEST_DATABASE_URL="postgres://gramstack@localhost/gramstack_core?host=$scratch&port=55439&sslmode=disable"
gateway_go test -race -count=1 -v ./internal/authorization ./internal/enforcement ./internal/consentmanager ./internal/consumer ./internal/authn ./internal/platform/outbox
DATABASE_URL="$GRAMSTACK_TEST_DATABASE_URL" gateway_go run ./cmd/gramstack migrate
