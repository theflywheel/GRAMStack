#!/bin/sh
set -eu

# Preserve the system roots for public OIDC and adapter discovery.
if [ -n "${GRAMSTACK_PRIVATE_CA_FILE:-}" ]; then
  umask 077
  bundle=$(mktemp /run/gramstack/roots.XXXXXX)
  cat /etc/ssl/certs/ca-certificates.crt "$GRAMSTACK_PRIVATE_CA_FILE" > "$bundle"
  export SSL_CERT_FILE="$bundle"
fi
exec /app/service "$@"
