#!/usr/bin/env bash
set -euo pipefail
for role in core adapter product hydra dedi; do
  secret="GRAMSTACK_DB_${role^^}_PASSWORD"
  if [[ -z "${!secret:-}" ]]; then echo "Required database role password is missing: $secret" >&2; exit 1; fi
done
for role in core adapter product hydra dedi; do
  secret="GRAMSTACK_DB_${role^^}_PASSWORD"
  export GRAMSTACK_ROLE_PASSWORD="${!secret}"
  psql --no-psqlrc --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" --set ON_ERROR_STOP=1 --set "role=$role" <<'SQL'
\getenv password GRAMSTACK_ROLE_PASSWORD
CREATE ROLE :"role" LOGIN PASSWORD :'password';
CREATE DATABASE :"role" OWNER :"role";
REVOKE CONNECT ON DATABASE :"role" FROM PUBLIC;
SQL
  unset GRAMSTACK_ROLE_PASSWORD
done
