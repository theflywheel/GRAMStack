#!/usr/bin/env bash
# Check the staged Railway database image and application migration commands.
set -euo pipefail
root=$(cd "$(dirname "$0")/../.." && pwd)
revision=$(git -C "$root" rev-parse --verify "${1:-HEAD}^{commit}")
for role in gateway adapter product; do
  image="gramstack-$role:deployment-check"
  if [[ $(docker image inspect --format '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$image") != "$revision" ]]; then
    echo "Build the three application images for this revision with scripts/test-images.sh first." >&2
    exit 1
  fi
done
umask 077
state=$(mktemp -d /tmp/gramstack-railway-db.XXXXXX)
name=$(basename "$state")
stage=''
cleanup() {
  docker rm -f "$name" >/dev/null 2>&1 || true
  docker network rm "$name" >/dev/null 2>&1 || true
  docker volume rm "$name" >/dev/null 2>&1 || true
  if [[ -n "$stage" ]]; then rm -rf "$stage"; fi
  rm -rf "$state"
}
trap cleanup EXIT

stage=$("$root/infra/railway/stage.sh" platform-db "$revision")
db_image="gramstack-platform-db:deployment-check"
docker build --progress=plain -f "$stage/infra/railway/database/Dockerfile" -t "$db_image" "$stage"
go run "$root/tools/devcerts/main.go" -railway-demo -dir "$state/certs"
python3 - "$state" <<'PY'
import base64
import json
from pathlib import Path
import secrets
import sys

root = Path(sys.argv[1])
roles = {role: secrets.token_hex(24) for role in ("core", "adapter", "product", "hydra", "dedi")}
(root / "db.env").write_text("POSTGRES_PASSWORD=" + secrets.token_hex(24) + "\n" + "".join(
    f"GRAMSTACK_DB_{role.upper()}_PASSWORD={password}\n" for role, password in roles.items()
))

def dsn(role):
    return f"postgres://{role}:{roles[role]}@platform-db:5432/{role}?sslmode=disable"

(root / "gateway.env").write_text("DATABASE_URL=" + dsn("core") + "\n")
(root / "product.env").write_text("SAKHI_DATABASE_URL=" + dsn("product") + "\n")
(root / "hydra.env").write_text("DSN=" + dsn("hydra") + "\n")
files = {filename: base64.b64encode((root / "certs" / filename).read_bytes()).decode()
         for filename in ("ca.pem", "lokos-adapter.pem", "lokos-adapter.key")}
(root / "adapter.env").write_text("\n".join((
    "ADAPTER_STATE_DATABASE_URL=" + dsn("adapter"),
    "ADAPTER_STATE_KEY=" + base64.b64encode(secrets.token_bytes(32)).decode(),
    "GRAMSTACK_FILES_JSON=" + json.dumps(files, separators=(",", ":")),
    "SERVICE_CA_FILE=/run/gramstack/ca.pem",
    "SERVICE_CERT_FILE=/run/gramstack/lokos-adapter.pem",
    "SERVICE_KEY_FILE=/run/gramstack/lokos-adapter.key",
    "LOKOS_SOURCE_SERVER_NAME=mock-lokos.gramstack.internal",
)) + "\n")
PY
docker network create --internal "$name" >/dev/null
docker volume create "$name" >/dev/null
docker run --rm --network none --mount "type=volume,source=$name,target=/var/lib/postgresql/data" \
  --entrypoint /bin/mkdir "$db_image" /var/lib/postgresql/data/lost+found
docker run -d --name "$name" --network "$name" --network-alias platform-db \
  --env-file "$state/db.env" --mount "type=volume,source=$name,target=/var/lib/postgresql/data" \
  "$db_image" >/dev/null
ready=false
for _ in $(seq 1 100); do
  if docker exec "$name" pg_isready -h 127.0.0.1 -U postgres -d postgres >/dev/null 2>&1; then
    ready=true
    break
  fi
  if [[ $(docker inspect --format "{{.State.Running}}" "$name") != true ]]; then break; fi
  sleep 0.2
done
if [[ "$ready" != true ]]; then
  docker logs "$name" >&2
  echo "Staged PostgreSQL did not become ready." >&2
  exit 1
fi

docker exec "$name" bash -ec '
  for role in core adapter product hydra dedi; do
    key="GRAMSTACK_DB_${role^^}_PASSWORD"
    export PGPASSWORD="${!key}"
    psql -X -h 127.0.0.1 -U "$role" -d "$role" -Atqc "SELECT 1" | grep -qx 1
    for other in core adapter product hydra dedi; do
      if [[ "$role" != "$other" ]] && psql -X -h 127.0.0.1 -U "$role" -d "$other" -Atqc "SELECT 1" >/dev/null 2>&1; then
        echo "A database role could connect to another role database." >&2
        exit 1
      fi
    done
  done
'
for _ in 1 2; do
  for role in gateway adapter product; do
    docker run --rm --network "$name" --env-file "$state/$role.env" \
      --entrypoint /app/runtimefiles "gramstack-$role:deployment-check" /app/service migrate
  done
  docker run --rm --network "$name" --env-file "$state/hydra.env" \
    oryd/hydra:v26.2.0@sha256:ff67c7fb5f95074fa53374d41151713554960504b340cd3f95b09e65deaea2a9 migrate sql up -e -y >/dev/null
done
echo "PASS: staged PostgreSQL 17, five isolated roles, and repeatable application and Hydra migrations"
