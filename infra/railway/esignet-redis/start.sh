#!/bin/sh
set -eu
case "${REDIS_PASSWORD:-}" in
  ''|*[!0-9a-fA-F]*) echo 'Redis requires a generated hex password' >&2; exit 1 ;;
esac
if [ "${#REDIS_PASSWORD}" -ne 64 ]; then
  echo 'Redis requires a 32-byte generated password' >&2
  exit 1
fi
umask 077
config=$(mktemp /tmp/gramstack-redis.XXXXXX)
printf 'appendonly yes\nrequirepass %s\n' "$REDIS_PASSWORD" > "$config"
if [ "$(id -u)" = 0 ]; then chown redis:redis "$config"; fi
unset REDIS_PASSWORD
exec /usr/local/bin/docker-entrypoint.sh redis-server "$config"
