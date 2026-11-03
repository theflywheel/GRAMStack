#!/usr/bin/env bash
# Isolated, real Hydra/PostgreSQL integration. No production URLs or credentials.
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
: "${PG_BIN:?Set PG_BIN to the PostgreSQL binary directory}"
: "${HYDRA_BIN:?Set HYDRA_BIN to an official Ory Hydra v26.2.0 executable}"
"$HYDRA_BIN" version | head -1 | grep -q 'v26.2.0'
scratch=$(mktemp -d /tmp/gramstack-hydra.XXXXXX)
hydra_pid=''
gateway_pid=''
idp_pid=''
cleanup(){
 if [[ -n "$gateway_pid" ]]; then kill "$gateway_pid" 2>/dev/null || true; wait "$gateway_pid" 2>/dev/null || true; fi
 if [[ -n "$hydra_pid" ]]; then kill "$hydra_pid" 2>/dev/null || true; wait "$hydra_pid" 2>/dev/null || true; fi
 if [[ -n "$idp_pid" ]]; then kill "$idp_pid" 2>/dev/null || true; wait "$idp_pid" 2>/dev/null || true; fi
 "$PG_BIN/pg_ctl" -D "$scratch/pg" -m immediate -w stop >/dev/null 2>&1 || true
 rm -rf "$scratch"
}
trap cleanup EXIT
"$PG_BIN/initdb" -D "$scratch/pg" -U gramstack --auth-local=trust --auth-host=scram-sha-256 > "$scratch/initdb.log"
"$PG_BIN/pg_ctl" -D "$scratch/pg" -l "$scratch/pg.log" -o "-k $scratch -p 55438 -c listen_addresses=''" -w start >/dev/null
for db in gramstack_core hydra; do "$PG_BIN/createdb" -h "$scratch" -p 55438 -U gramstack "$db"; done
export GRAMSTACK_TEST_DATABASE_URL="postgres://gramstack@localhost/gramstack_core?host=$scratch&port=55438&sslmode=disable"
export DSN="postgres://gramstack@localhost/hydra?host=$scratch&port=55438&sslmode=disable"
"$HYDRA_BIN" migrate sql up -e -y > "$scratch/hydra-migrate.log" 2>&1
free_port(){ python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()'; }
public_port=$(free_port)
admin_port=$(free_port)
gateway_port=$(free_port)
idp_port=$(free_port)
export GRAMSTACK_TEST_GATEWAY_URL="http://127.0.0.1:$gateway_port"
export URLS_LOGIN="$GRAMSTACK_TEST_GATEWAY_URL/auth/login" URLS_CONSENT="$GRAMSTACK_TEST_GATEWAY_URL/auth/oauth-consent"
export GRAMSTACK_TEST_HYDRA_PUBLIC="http://127.0.0.1:$public_port"
export GRAMSTACK_TEST_HYDRA_ADMIN="http://127.0.0.1:$admin_port"
export URLS_SELF_ISSUER="$GRAMSTACK_TEST_HYDRA_PUBLIC/"
export SERVE_PUBLIC_HOST=127.0.0.1 SERVE_PUBLIC_PORT="$public_port" SERVE_ADMIN_HOST=127.0.0.1 SERVE_ADMIN_PORT="$admin_port"
export SECRETS_SYSTEM='isolated-test-secret-not-for-production-0001'
export LOG_LEVEL=error
"$HYDRA_BIN" serve all --dev --sqa-opt-out > "$scratch/hydra.log" 2>&1 &
hydra_pid=$!
ready=false
for _ in $(seq 1 150); do
 if curl -fsS "$GRAMSTACK_TEST_HYDRA_ADMIN/health/ready" >/dev/null 2>&1; then ready=true; break; fi
 if ! kill -0 "$hydra_pid" 2>/dev/null; then echo 'Isolated Hydra exited before readiness' >&2; cat "$scratch/hydra.log" >&2; exit 1; fi
 sleep 0.1
done
if [[ "$ready" != true ]]; then echo 'Isolated Hydra did not become ready' >&2; exit 1; fi
GOWORK=off go -C "$root/services/gateway" test -race -count=1 -v -run TestActualHydraConsumerAPI ./internal/consumer
DATABASE_URL="$GRAMSTACK_TEST_DATABASE_URL" GOWORK=off go -C "$root/services/gateway" run ./cmd/gramstack migrate
GOWORK=off go -C "$root/services/gateway" build -o "$scratch/gateway" ./cmd/gramstack
export GRAMSTACK_CONFIG_FILE="$scratch/gateway.json" GRAMSTACK_ENV=development DATABASE_URL="$GRAMSTACK_TEST_DATABASE_URL" PORT="$gateway_port"
python3 - <<'PY'
import json,os,pathlib
policy=dict(registry='lokos',resource='shg.membership',fields=['membership_status','savings_summary'],purposes=['credit_eligibility'],hint_types=['member_reference'],terms_version='terms-v1',terms_text='Use for the declared assessment only.')
config=dict(public_origin='http://127.0.0.1:'+os.environ['PORT'],hydra_introspection_url=os.environ['GRAMSTACK_TEST_HYDRA_ADMIN']+'/admin/oauth2/introspect',hydra_issuer=os.environ['GRAMSTACK_TEST_HYDRA_PUBLIC']+'/',audience='gramstack',scope='gramstack.access',allow_loopback_http=True,consumers={'consumer-one':[policy]})
pathlib.Path(os.environ['GRAMSTACK_CONFIG_FILE']).write_text(json.dumps(config))
PY
if [[ "${GRAMSTACK_TEST_AUTHN:-}" == 1 ]]; then
 export GRAMSTACK_TEST_IDP_URL="http://127.0.0.1:$idp_port"
 export OIDC_ISSUER="$GRAMSTACK_TEST_IDP_URL" OIDC_CLIENT_ID="gramstack-upstream" OIDC_CLIENT_SECRET="isolated-upstream-secret" OIDC_REDIRECT_URI="$GRAMSTACK_TEST_GATEWAY_URL/auth/provider/callback"
 export GRAMSTACK_AUTH_KEY=$(python3 -c 'import base64,secrets; print(base64.b64encode(secrets.token_bytes(32)).decode())')
 export GRAMSTACK_PORTAL_CLIENT_SECRET="isolated-portal-secret" GRAMSTACK_UPSTREAM_CLIENT_SECRET="$OIDC_CLIENT_SECRET"
 GOWORK=off go -C "$root/mocks/identity" build -o "$scratch/demo-idp" ./cmd/demo-idp
 PORT="$idp_port" "$scratch/demo-idp" > "$scratch/idp.log" 2>&1 &
 idp_pid=$!
 ready=false
 for _ in $(seq 1 100); do
  if curl -fsS "$GRAMSTACK_TEST_IDP_URL/healthz" >/dev/null 2>&1; then ready=true; break; fi
  sleep 0.1
 done
 if [[ "$ready" != true ]]; then echo 'Demo IdP failed startup' >&2; cat "$scratch/idp.log" >&2; exit 1; fi
 python3 - <<'PYA'
import json,os,pathlib,urllib.request
path=pathlib.Path(os.environ['GRAMSTACK_CONFIG_FILE']);config=json.loads(path.read_text())
config['authentication']=dict(client_id='gramstack-portal',hydra_admin_url=os.environ['GRAMSTACK_TEST_HYDRA_ADMIN'],upstream_issuer=os.environ['GRAMSTACK_TEST_IDP_URL'],upstream_client_id='gramstack-upstream',demo_upstream=True)
path.write_text(json.dumps(config))
body=dict(client_id='gramstack-portal',client_secret=os.environ['GRAMSTACK_PORTAL_CLIENT_SECRET'],grant_types=['authorization_code'],response_types=['code'],scope='openid',redirect_uris=[os.environ['GRAMSTACK_TEST_GATEWAY_URL']+'/auth/callback'],token_endpoint_auth_method='client_secret_basic')
with urllib.request.urlopen(urllib.request.Request(os.environ['GRAMSTACK_TEST_HYDRA_ADMIN']+'/admin/clients',data=json.dumps(body).encode(),headers={'Content-Type':'application/json'})) as response: assert response.status==201
PYA
fi
"$scratch/gateway" > "$scratch/gateway.log" 2>&1 &
gateway_pid=$!
ready=false
for _ in $(seq 1 100); do
 if curl -fsS "http://127.0.0.1:$gateway_port/healthz" >/dev/null 2>&1; then ready=true; break; fi
 sleep 0.1
done
if [[ "$ready" != true ]]; then echo 'Configured gateway failed startup' >&2; cat "$scratch/gateway.log" >&2; exit 1; fi
python3 - <<'PY'
import base64,json,os,urllib.request,urllib.parse,urllib.error
public=os.environ['GRAMSTACK_TEST_HYDRA_PUBLIC'];base='http://127.0.0.1:'+os.environ['PORT']
auth='Basic '+base64.b64encode(b'consumer-one:isolated-synthetic-secret').decode()
req=urllib.request.Request(public+'/oauth2/token',data=urllib.parse.urlencode(dict(grant_type='client_credentials',scope='gramstack.access',audience='gramstack')).encode(),headers={'Authorization':auth,'Content-Type':'application/x-www-form-urlencoded'})
with urllib.request.urlopen(req) as res: token=json.load(res)['access_token']
body=dict(registry='lokos',resource='shg.membership',subject_hint=dict(type='member_reference',value='fictional-member'),fields=['membership_status'],purpose='credit_eligibility',access=dict(mode='single_use',valid_for_seconds=300),recipient_terms=dict(retention_policy_id='terms-v1'))
headers={'Authorization':'Bearer '+token,'Content-Type':'application/json','Idempotency-Key':'process-smoke'}
with urllib.request.urlopen(urllib.request.Request(base+'/v1/access-requests',data=json.dumps(body).encode(),headers=headers)) as res:
 assert res.status==202; accepted=json.load(res)
with urllib.request.urlopen(urllib.request.Request(accepted['status_url'],headers={'Authorization':'Bearer '+token})) as res: assert json.load(res)['status']=='AUTHENTICATION_PENDING'
with urllib.request.urlopen(urllib.request.Request(accepted['status_url']+'/cancel',data=b'',headers={'Authorization':'Bearer '+token})) as res: assert json.load(res)['status']=='CANCELLED'
try: urllib.request.urlopen(accepted['status_url'])
except urllib.error.HTTPError as error: assert error.code==401
else: raise AssertionError('unauthenticated access accepted')
print('PASS: real Hydra client credentials -> running gateway create/status/cancel; anonymous access rejected')
PY
if [[ "${GRAMSTACK_TEST_AUTHN:-}" == 1 ]]; then
 GOWORK=off go -C "$root/services/gateway" test -race -count=1 -v -run TestActualFederation ./internal/authn
fi
kill -TERM "$gateway_pid"
wait "$gateway_pid"
gateway_pid=''
