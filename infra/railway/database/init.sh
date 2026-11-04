#!/usr/bin/env bash
set -euo pipefail
for role in core adapter product hydra dedi esignet esignet_mock; do
  secret="GRAMSTACK_DB_${role^^}_PASSWORD"
  if [[ -z "${!secret:-}" ]]; then echo "Required database role password is missing: $secret" >&2; exit 1; fi
done
for role in core adapter product hydra dedi esignet esignet_mock; do
  secret="GRAMSTACK_DB_${role^^}_PASSWORD"
  export GRAMSTACK_ROLE_PASSWORD="${!secret}"
  psql --no-psqlrc --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" --set ON_ERROR_STOP=1 --set "role=$role" <<'SQL'
\getenv password GRAMSTACK_ROLE_PASSWORD
CREATE ROLE :"role" LOGIN PASSWORD :'password';
SQL
  if [[ "$role" != esignet && "$role" != esignet_mock ]]; then
    psql --no-psqlrc --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" --set ON_ERROR_STOP=1 --set "role=$role" <<'SQL'
CREATE DATABASE :"role" OWNER :"role";
REVOKE CONNECT ON DATABASE :"role" FROM PUBLIC;
SQL
  fi
  unset GRAMSTACK_ROLE_PASSWORD
done
psql --no-psqlrc --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" --set ON_ERROR_STOP=1 \
  --set esignet_owner=esignet --set mock_owner=esignet_mock --file /usr/local/share/esignet-init.sql
