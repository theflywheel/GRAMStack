#!/usr/bin/env bash
set -euo pipefail
root=$(cd "$(dirname "$0")/../.." && pwd)
pg=${PG_BIN:?Set PG_BIN to the PostgreSQL binary directory}
hydra=${HYDRA_BIN:?Set HYDRA_BIN to the official Hydra v26.2.0 binary}
cd "$root"
scratch=$(mktemp -d /tmp/gramstack-hydra-split.XXXXXX)
pids=()
cleanup(){
 for pid in "${pids[@]}"; do kill -TERM "$pid" 2>/dev/null || true; wait "$pid" 2>/dev/null || true; done
 "$pg/pg_ctl" -D "$scratch/pg" -m fast -w stop >/dev/null 2>&1 || true
}
trap cleanup EXIT
echo "Private Hydra probe logs: $scratch"
read -r pub admin socket_port <<< "$(python3 -c 'import socket; s=[socket.socket() for _ in range(3)]; [x.bind(("127.0.0.1",0)) for x in s]; print(*(x.getsockname()[1] for x in s))')"
LC_ALL=C "$pg/initdb" -D "$scratch/pg" -U gramstack --auth-local=trust --auth-host=scram-sha-256 > "$scratch/pg-init.log"
LC_ALL=C "$pg/pg_ctl" -D "$scratch/pg" -l "$scratch/pg.log" -o "-k $scratch -p $socket_port -c listen_addresses=''" -w start >/dev/null
"$pg/createdb" -h "$scratch" -p "$socket_port" -U gramstack hydra
GOWORK=off go run ./tools/devcerts/main.go -dir "$scratch/certs"
export DSN="postgres://gramstack@localhost/hydra?host=$scratch&port=$socket_port&sslmode=disable"
export SECRETS_SYSTEM=$(openssl rand -hex 32)
export URLS_SELF_ISSUER=https://oauth.example.invalid/
export SERVE_PUBLIC_HOST=127.0.0.1 SERVE_PUBLIC_PORT="$pub" SERVE_ADMIN_HOST=127.0.0.1 SERVE_ADMIN_PORT="$admin"
"$hydra" migrate sql up -e -y > "$scratch/migrate.log" 2>&1
SERVE_TLS_ENABLED=true SERVE_TLS_CERT_BASE64="$(openssl base64 -A -in "$scratch/certs/gateway.pem")" SERVE_TLS_KEY_BASE64="$(openssl base64 -A -in "$scratch/certs/gateway.key")" "$hydra" serve admin --sqa-opt-out > "$scratch/admin.log" 2>&1 &
pids+=("$!")
SERVE_TLS_ENABLED=false "$hydra" serve public --sqa-opt-out > "$scratch/public.log" 2>&1 &
pids+=("$!")
export PROBE_PUB="$pub" PROBE_ADMIN="$admin" PROBE_CA="$scratch/certs/ca.pem"
python3 - <<'PY'
import base64,json,os,secrets,ssl,time
from urllib.request import Request,urlopen
from urllib.error import HTTPError
pub='http://127.0.0.1:'+os.environ['PROBE_PUB']
admin='https://127.0.0.1:'+os.environ['PROBE_ADMIN']
context=ssl.create_default_context(cafile=os.environ['PROBE_CA'])
proxy={'X-Forwarded-Proto':'https'}
for _ in range(80):
 try:
  with urlopen(admin+'/health/ready',context=context,timeout=1) as response: assert response.status==200
  with urlopen(Request(pub+'/.well-known/openid-configuration',headers=proxy),timeout=1) as response:
   assert json.load(response)['issuer']=='https://oauth.example.invalid/'
  break
 except OSError:
  time.sleep(.15)
else: raise RuntimeError('split listener readiness failed')
with urlopen(pub+'/health/ready',timeout=4) as response: assert response.status==200
secret=secrets.token_hex(32)
body=dict(client_id='probe',client_secret=secret,grant_types=['client_credentials'],scope='gramstack.access',audience=['gramstack'],token_endpoint_auth_method='client_secret_basic')
with urlopen(Request(admin+'/admin/clients',json.dumps(body).encode(),{'Content-Type':'application/json'}),context=context,timeout=4) as response: assert response.status==201
header=dict(proxy,Authorization='Basic '+base64.b64encode(('probe:'+secret).encode()).decode(),**{'Content-Type':'application/x-www-form-urlencoded'})
with urlopen(Request(pub+'/oauth2/token',b'grant_type=client_credentials&scope=gramstack.access&audience=gramstack',header),timeout=4) as response: token=json.load(response)['access_token']
from urllib.parse import urlencode
with urlopen(Request(admin+'/admin/oauth2/introspect',urlencode(dict(token=token)).encode(),{'Content-Type':'application/x-www-form-urlencoded'}),context=context,timeout=4) as response:
 result=json.load(response); assert result['active'] and result['client_id']=='probe'
try:
 urlopen(Request(pub+'/admin/clients',headers=proxy),timeout=4)
except HTTPError as error:
 assert error.code==404
else: raise AssertionError('Public admin route present')
print('PASS: base64 TLS admin credentials, shared client and token state, private introspection, public admin denial')
PY
