#!/bin/sh
set -eu
volume=$1
entrypoint=$2
shift 2
chown 1001:1001 "$volume"
chmod 0700 "$volume"
exec setpriv --reuid=1001 --regid=1001 --clear-groups "$entrypoint" "$@"
