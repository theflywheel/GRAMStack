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
"$PG_BIN/initdb" -D "$scratch/pg" -U gramstack --auth-local=trust --auth-host=scram-sha-256 > "$scratch/initdb.log"
"$PG_BIN/pg_ctl" -D "$scratch/pg" -l "$scratch/pg.log" -o "-k $scratch -p 55439 -c listen_addresses=''" -w start >/dev/null
"$PG_BIN/createdb" -h "$scratch" -p 55439 -U gramstack gramstack_core
export GRAMSTACK_TEST_DATABASE_URL="postgres://gramstack@localhost/gramstack_core?host=$scratch&port=55439&sslmode=disable"
GOWORK=off go -C "$root/services/gateway" test -race -count=1 -v ./internal/enforcement ./internal/consentmanager ./internal/consumer
DATABASE_URL="$GRAMSTACK_TEST_DATABASE_URL" GOWORK=off go -C "$root/services/gateway" run ./cmd/gramstack migrate
