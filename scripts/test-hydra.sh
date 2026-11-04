#!/usr/bin/env bash
# Isolated, real Hydra/PostgreSQL integration. No production URLs or credentials.
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
if [[ "${GRAMSTACK_TEST_PRODUCT_UI:-}" == 1 ]]; then export GRAMSTACK_TEST_PRODUCT=1; fi
if [[ "${GRAMSTACK_TEST_PRODUCT:-}" == 1 ]]; then export GRAMSTACK_TEST_DISCOVERY=1; fi
if [[ "${GRAMSTACK_TEST_ANCHORING:-}" == 1 ]]; then export GRAMSTACK_TEST_DISCOVERY=1; fi
if [[ "${GRAMSTACK_TEST_DISCOVERY:-}" == 1 ]]; then export GRAMSTACK_TEST_RETRIEVAL=1; fi
if [[ "${GRAMSTACK_TEST_RETRIEVAL:-}" == 1 ]]; then export GRAMSTACK_TEST_CUSTODY=1; fi
if [[ "${GRAMSTACK_TEST_CUSTODY:-}" == 1 ]]; then export GRAMSTACK_TEST_AUTHZ=1; fi
if [[ "${GRAMSTACK_TEST_AUTHZ:-}" == 1 ]]; then export GRAMSTACK_TEST_WORKERS=1; fi
if [[ "${GRAMSTACK_PREVIEW:-}" == 1 && "${GRAMSTACK_TEST_PORTAL:-}" != 1 && "${GRAMSTACK_TEST_WORKERS:-}" != 1 ]]; then
 echo "Preview requires GRAMSTACK_TEST_PORTAL=1 or GRAMSTACK_TEST_WORKERS=1" >&2; exit 1
fi
if [[ "${GRAMSTACK_TEST_WORKERS:-}" == 1 ]]; then export GRAMSTACK_TEST_PORTAL=1; fi
if [[ "${GRAMSTACK_TEST_PORTAL:-}" == 1 ]]; then export GRAMSTACK_TEST_BINDING=1; fi
if [[ "${GRAMSTACK_TEST_BINDING:-}" == 1 ]]; then export GRAMSTACK_TEST_AUTHN=1; fi
if [[ "${GRAMSTACK_TEST_ESIGNET:-}" == 1 ]]; then export GRAMSTACK_TEST_AUTHN=1; fi
: "${PG_BIN:?Set PG_BIN to the PostgreSQL binary directory}"
: "${HYDRA_BIN:?Set HYDRA_BIN to an official Ory Hydra v26.2.0 executable}"
"$HYDRA_BIN" version | awk 'NR == 1 && $NF == "v26.2.0" {found=1} END {exit !found}'
scratch=$(mktemp -d /tmp/gramstack-hydra.XXXXXX)
hydra_pid=''
gateway_pid=''
idp_pid=''
adapter_pid=''
source_pid=''
product_pid=''
cleanup(){
 if [[ -n "$product_pid" ]]; then kill "$product_pid" 2>/dev/null || true; wait "$product_pid" 2>/dev/null || true; fi
 for pid in "$adapter_pid" "$source_pid"; do if [[ -n "$pid" ]]; then kill "$pid" 2>/dev/null || true; wait "$pid" 2>/dev/null || true; fi; done
 if [[ -n "$gateway_pid" ]]; then kill "$gateway_pid" 2>/dev/null || true; wait "$gateway_pid" 2>/dev/null || true; fi
 if [[ -n "$hydra_pid" ]]; then kill "$hydra_pid" 2>/dev/null || true; wait "$hydra_pid" 2>/dev/null || true; fi
 if [[ -n "$idp_pid" ]]; then kill "$idp_pid" 2>/dev/null || true; wait "$idp_pid" 2>/dev/null || true; fi
 "$PG_BIN/pg_ctl" -D "$scratch/pg" -m immediate -w stop >/dev/null 2>&1 || true
 rm -rf "$scratch"
}
trap cleanup EXIT
source "$root/scripts/gateway-module.sh"
prepare_gateway_module "$root" "$scratch"
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
gateway_go test -race -count=1 -v -run TestActualHydraConsumerAPI ./internal/consumer
gateway_go build -o "$scratch/gateway" ./cmd/gramstack
DATABASE_URL="$GRAMSTACK_TEST_DATABASE_URL" "$scratch/gateway" migrate
export GRAMSTACK_CONFIG_FILE="$scratch/gateway.json" GRAMSTACK_ENV=development DATABASE_URL="$GRAMSTACK_TEST_DATABASE_URL" PORT="$gateway_port"
python3 - <<'PY'
import json,os,pathlib
policy=dict(registry='lokos',resource='shg.membership',fields=['membership_status','savings_summary'],purposes=['credit_eligibility'],hint_types=['member_reference'],terms_version='terms-v1',max_access_seconds=300,terms_text='Use only for this credit eligibility assessment. Retain approved data for up to 30 days, then delete it unless a documented legal retention requirement applies. No onward sharing.')
config=dict(public_origin='http://127.0.0.1:'+os.environ['PORT'],hydra_introspection_url=os.environ['GRAMSTACK_TEST_HYDRA_ADMIN']+'/admin/oauth2/introspect',hydra_issuer=os.environ['GRAMSTACK_TEST_HYDRA_PUBLIC']+'/',audience='gramstack',scope='gramstack.access',allow_loopback_http=True,consumers={'consumer-one':[policy]})
pathlib.Path(os.environ['GRAMSTACK_CONFIG_FILE']).write_text(json.dumps(config))
PY
setup_product(){
 GOWORK=off go -C "$root/apps/sakhicredit" build -o "$scratch/sakhicredit" ./cmd/sakhicredit
 "$PG_BIN/createdb" -h "$scratch" -p 55438 -U gramstack sakhicredit
 export SAKHI_DATABASE_URL="postgres://gramstack@localhost/sakhicredit?host=$scratch&port=55438&sslmode=disable"
 "$scratch/sakhicredit" migrate
 local product_port=$(free_port)
 export GRAMSTACK_TEST_PRODUCT_URL="http://127.0.0.1:$product_port" GRAMSTACK_TEST_PRODUCT_RESUME="$scratch/product-resume.json"
 export SAKHI_CONFIG_FILE="$scratch/product.json" SAKHI_STORAGE_KEY_FILE="$scratch/product-storage.key" SAKHI_ENV=development
 export SAKHI_SERVICE_CA_FILE="$SERVICE_CA_FILE" SAKHI_CONSUMER_CERT_FILE="$scratch/certs/consumer-one.pem" SAKHI_CONSUMER_KEY_FILE="$scratch/certs/consumer-one.key"
 export SAKHI_CLIENT_SECRET='isolated-synthetic-secret'
 python3 - <<'PYS'
import json,os,pathlib
pathlib.Path(os.environ['SAKHI_STORAGE_KEY_FILE']).write_bytes(os.urandom(32));pathlib.Path(os.environ['SAKHI_STORAGE_KEY_FILE']).chmod(0o600)
config=dict(public_origin=os.environ['GRAMSTACK_TEST_PRODUCT_URL'],gateway_origin=os.environ['GRAMSTACK_TEST_GATEWAY_URL'],gateway_data_origin=os.environ['GRAMSTACK_TEST_AUTHZ_URL'],hydra_token_url=os.environ['GRAMSTACK_TEST_HYDRA_PUBLIC']+'/oauth2/token',client_id='consumer-one',data_server_name='gateway.gramstack.internal',allow_loopback_http=True)
pathlib.Path(os.environ['SAKHI_CONFIG_FILE']).write_text(json.dumps(config))
PYS
}
start_product(){
 PORT="${GRAMSTACK_TEST_PRODUCT_URL##*:}" "$scratch/sakhicredit" > "$scratch/product.log" 2>&1 &
 product_pid=$!
 local ready=false
 for _ in $(seq 1 100); do
  if curl -fsS "$GRAMSTACK_TEST_PRODUCT_URL/readyz" >/dev/null 2>&1; then ready=true; break; fi
  sleep 0.1
 done
 if [[ "$ready" != true ]]; then echo 'SakhiCredit failed readiness' >&2; cat "$scratch/product.log" >&2; exit 1; fi
}
stop_product(){ kill -TERM "$product_pid"; wait "$product_pid"; product_pid=''; }
start_gateway(){
"$scratch/gateway" > "$scratch/gateway.log" 2>&1 &
gateway_pid=$!
ready=false
for _ in $(seq 1 100); do
 if curl -fsS "http://127.0.0.1:$gateway_port/readyz" >/dev/null 2>&1; then ready=true; break; fi
 sleep 0.1
done
if [[ "$ready" != true ]]; then echo 'Configured gateway failed startup' >&2; cat "$scratch/gateway.log" >&2; exit 1; fi
}
stop_gateway(){
 kill -TERM "$gateway_pid"
 wait "$gateway_pid"
 gateway_pid=''
}
start_binding(){
 SERVICE_CERT_FILE="$scratch/certs/mock-lokos.pem" SERVICE_KEY_FILE="$scratch/certs/mock-lokos.key" OIDC_ISSUER="$GRAMSTACK_TEST_IDP_URL" \
 OIDC_SUBJECT_BINDINGS_FILE="$scratch/source-bindings.json" SOURCE_TLS_PORT="$source_port" MOCK_VERIFICATION_GATE_DIRECTORY="$scratch/source-gate" MOCK_READ_GATE_DIRECTORY="$scratch/source-read-gate" \
 "$scratch/mock-lokos" > "$scratch/source.log" 2>&1 &
 source_pid=$!
 SERVICE_CERT_FILE="$scratch/certs/lokos-adapter.pem" SERVICE_KEY_FILE="$scratch/certs/lokos-adapter.key" PORT="$adapter_health_port" \
 "$scratch/lokos-adapter" > "$scratch/adapter.log" 2>&1 &
 adapter_pid=$!
 local ready=false
 for _ in $(seq 1 100); do
  if curl -fsS "http://127.0.0.1:$adapter_health_port/readyz" >/dev/null 2>&1 && curl -fsS --cacert "$SERVICE_CA_FILE" --cert "$scratch/certs/lokos-adapter.pem" --key "$scratch/certs/lokos-adapter.key" "https://127.0.0.1:$source_port/healthz" >/dev/null 2>&1 && \
    [[ $(curl -s --cacert "$SERVICE_CA_FILE" --cert "$SERVICE_CERT_FILE" --key "$SERVICE_KEY_FILE" -o /dev/null -w '%{http_code}' "$GRAMSTACK_TEST_ADAPTER_URL/internal/v1/subject-verifications") == 404 ]]; then ready=true; break; fi
  sleep 0.1
 done
 if [[ "$ready" != true ]]; then echo 'Binding services failed startup' >&2; cat "$scratch/adapter.log" "$scratch/source.log" >&2; exit 1; fi
}
stop_binding(){
 for pid in "$adapter_pid" "$source_pid"; do kill -TERM "$pid"; wait "$pid"; done
 adapter_pid='';source_pid=''
}

start_gateway
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
stop_gateway
if [[ "${GRAMSTACK_TEST_AUTHN:-}" == 1 ]]; then
 export GRAMSTACK_AUTH_KEY=$(python3 -c 'import base64,secrets; print(base64.b64encode(secrets.token_bytes(32)).decode())')
 export GRAMSTACK_PORTAL_CLIENT_SECRET="isolated-portal-secret"
 if [[ "${GRAMSTACK_TEST_ESIGNET:-}" == 1 ]]; then
  export GRAMSTACK_TEST_ESIGNET_API_URL="http://127.0.0.1:${ESIGNET_DEMO_API_PORT:-19080}"
  export GRAMSTACK_TEST_IDP_URL="http://127.0.0.1:${ESIGNET_DEMO_ISSUER_PORT:-${ESIGNET_DEMO_API_PORT:-19080}}"
  export GRAMSTACK_TEST_ESIGNET_UI="http://127.0.0.1:${ESIGNET_DEMO_UI_PORT:-19300}"
  unset GRAMSTACK_UPSTREAM_CLIENT_SECRET
  export GRAMSTACK_UPSTREAM_PRIVATE_KEY_FILE="$scratch/esignet-client.pem"
  GOWORK=off go -C "$root/adapters/auth/oidc" build -o "$scratch/esignet-demo" "$root/tools/esignet-demo/main.go" "$root/tools/esignet-demo/bindings.go"
  if GRAMSTACK_ENV=production "$scratch/esignet-demo" -callback "$GRAMSTACK_TEST_GATEWAY_URL/auth/provider/callback" -directory "$scratch" >/dev/null 2>&1; then
   echo 'Demo onboarding allowed outside development' >&2; exit 1
  fi
  "$scratch/esignet-demo" -callback "$GRAMSTACK_TEST_GATEWAY_URL/auth/provider/callback" -directory "$scratch"
 else
  export GRAMSTACK_TEST_IDP_URL="http://127.0.0.1:$idp_port"
  export OIDC_FORM_RETURN_ORIGINS="$GRAMSTACK_TEST_HYDRA_PUBLIC" OIDC_ISSUER="$GRAMSTACK_TEST_IDP_URL" OIDC_CLIENT_ID="gramstack-upstream" OIDC_CLIENT_SECRET="isolated-upstream-secret" OIDC_REDIRECT_URI="$GRAMSTACK_TEST_GATEWAY_URL/auth/provider/callback"
  export GRAMSTACK_UPSTREAM_CLIENT_SECRET="$OIDC_CLIENT_SECRET"
  unset GRAMSTACK_UPSTREAM_PRIVATE_KEY_FILE
  GOWORK=off go -C "$root/mocks/identity" build -o "$scratch/demo-idp" ./cmd/demo-idp
  PORT="$idp_port" "$scratch/demo-idp" > "$scratch/idp.log" 2>&1 &
  idp_pid=$!
  ready=false
  for _ in $(seq 1 100); do
   if curl -fsS "$GRAMSTACK_TEST_IDP_URL/healthz" >/dev/null 2>&1; then ready=true; break; fi
   sleep 0.1
  done
  if [[ "$ready" != true ]]; then echo 'Demo IdP failed startup' >&2; exit 1; fi
 fi
 python3 - <<'PYA'
import json,os,pathlib,urllib.request
path=pathlib.Path(os.environ['GRAMSTACK_CONFIG_FILE']);config=json.loads(path.read_text())
if os.environ.get('GRAMSTACK_TEST_ESIGNET')=='1':
 provider=json.loads((path.parent/'esignet-profile.json').read_text())
else:
 provider=dict(schema_version='gramstack.auth.oidc/v1',issuer=os.environ['GRAMSTACK_TEST_IDP_URL'],client_id='gramstack-upstream',token_endpoint_auth_method='client_secret_basic',id_token_algorithms=['RS256'])
config['authentication']=dict(client_id='gramstack-portal',hydra_admin_url=os.environ['GRAMSTACK_TEST_HYDRA_ADMIN'],provider=provider,demo_upstream=True)
path.write_text(json.dumps(config))
body=dict(client_id='gramstack-portal',client_secret=os.environ['GRAMSTACK_PORTAL_CLIENT_SECRET'],grant_types=['authorization_code'],response_types=['code'],scope='openid',redirect_uris=[os.environ['GRAMSTACK_TEST_GATEWAY_URL']+'/auth/callback'],token_endpoint_auth_method='client_secret_basic')
with urllib.request.urlopen(urllib.request.Request(os.environ['GRAMSTACK_TEST_HYDRA_ADMIN']+'/admin/clients',data=json.dumps(body).encode(),headers={'Content-Type':'application/json'})) as response: assert response.status==201
PYA
 if [[ "${GRAMSTACK_TEST_BINDING:-}" == 1 ]]; then
  "$PG_BIN/createdb" -h "$scratch" -p 55438 -U gramstack gramstack_adapter
  export GRAMSTACK_TEST_WORKER_RESUME="$scratch/worker-resume.json"
  export GRAMSTACK_TEST_ADAPTER_DATABASE_URL="postgres://gramstack@localhost/gramstack_adapter?host=$scratch&port=55438&sslmode=disable"
  export ADAPTER_STATE_DATABASE_URL="$GRAMSTACK_TEST_ADAPTER_DATABASE_URL" ADAPTER_DEMO_VERIFIER=true
  export ADAPTER_STATE_KEY=$(python3 -c 'import base64,secrets; print(base64.b64encode(secrets.token_bytes(32)).decode())')
  export SERVICE_CA_FILE="$scratch/certs/ca.pem" SERVICE_CERT_FILE="$scratch/certs/gateway.pem" SERVICE_KEY_FILE="$scratch/certs/gateway.key"
  export GRAMSTACK_TEST_CERTS="$scratch/certs" GRAMSTACK_TEST_BINDING_RESUME="$scratch/binding-resume.json" GRAMSTACK_TEST_SOURCE_GATE="$scratch/source-gate"
  mkdir -m 700 "$scratch/source-gate" "$scratch/source-read-gate"
  export GRAMSTACK_TEST_SOURCE_READ_GATE="$scratch/source-read-gate"
  source_port=$(free_port);adapter_port=$(free_port);adapter_health_port=$(free_port)
  export LOKOS_SOURCE_URL="https://127.0.0.1:$source_port" LOKOS_SOURCE_SERVER_NAME="mock-lokos.gramstack.internal" ADAPTER_TLS_PORT="$adapter_port"
  export GRAMSTACK_TEST_ADAPTER_PUBLIC="http://127.0.0.1:$adapter_health_port" GRAMSTACK_TEST_ADAPTER_URL="https://127.0.0.1:$adapter_port" GRAMSTACK_TEST_SOURCE_URL="$LOKOS_SOURCE_URL"
  GOWORK=off go run "$root/tools/devcerts/main.go" -dir "$scratch/certs"
  cp "$root/adapters/lokos/go.mod" "$scratch/lokos.mod"; cp "$root/adapters/lokos/go.sum" "$scratch/lokos.sum"
  GOWORK=off go -C "$root/adapters/lokos" mod edit -modfile="$scratch/lokos.mod" -replace="github.com/theflywheel/GRAMStack/sdk/go/adapter=$root/sdk/go/adapter"
  GOWORK=off go -C "$root/adapters/lokos" build -modfile="$scratch/lokos.mod" -o "$scratch/lokos-adapter" ./cmd/lokos-adapter
  GOWORK=off go -C "$root/mocks/lokos" build -o "$scratch/mock-lokos" ./cmd/mock-lokos
  SERVICE_CERT_FILE="$scratch/certs/lokos-adapter.pem" SERVICE_KEY_FILE="$scratch/certs/lokos-adapter.key" "$scratch/lokos-adapter" migrate
  python3 - <<'PYB'
import json,os,pathlib
configpath=pathlib.Path(os.environ['GRAMSTACK_CONFIG_FILE']);config=json.loads(configpath.read_text())
config['registries']=[dict(registry='lokos',origin=os.environ['GRAMSTACK_TEST_ADAPTER_URL'],server_name='lokos-adapter.gramstack.internal',verification_method='demo.registry-map-v1',verification_contract_version='0.1.0',demo=True)]
configpath.write_text(json.dumps(config))
if os.environ.get('GRAMSTACK_TEST_ESIGNET')=='1':
 bindings=json.loads((configpath.parent/'esignet-bindings.json').read_text())
else:
 bindings={'member-12345':'demo-sunita','member-67890':'demo-rani'}
(configpath.parent/'source-bindings.json').write_text(json.dumps(bindings))
PYB
  if [[ "${GRAMSTACK_TEST_PORTAL:-}" == 1 ]]; then
   export GRAMSTACK_CONSENT_SIGNING_KEY_FILE="$scratch/certs/consent-signing.key"
   python3 - <<'PYC'
import json,os,pathlib
path=pathlib.Path(os.environ['GRAMSTACK_CONFIG_FILE']);config=json.loads(path.read_text())
config['consent']=dict(key_id='demo-consent-key')
path.write_text(json.dumps(config))
PYC
  fi
  if [[ "${GRAMSTACK_TEST_AUTHZ:-}" == 1 ]]; then
   authz_port=$(free_port)
   export GRAMSTACK_TEST_AUTHZ_URL="https://127.0.0.1:$authz_port" GRAMSTACK_GRANT_SIGNING_KEY_FILE="$scratch/certs/grant-signing.key"
   export GRAMSTACK_TEST_AUTHZ_RESUME="$scratch/authz-resume.json" GRAMSTACK_TEST_CUSTODY_RESUME="$scratch/custody-resume.json" GRAMSTACK_TEST_RETRIEVAL_RESUME="$scratch/retrieval-resume.json"
   if [[ "${GRAMSTACK_TEST_RETRIEVAL:-}" == 1 ]]; then export ADAPTER_RETRIEVAL_CONFIG_FILE="$scratch/adapter-retrieval.json" ADAPTER_EVIDENCE_SIGNING_KEY_FILE="$scratch/certs/adapter-evidence.key"; fi
   python3 - <<'PYZ'
import json,os,pathlib,urllib.parse
path=pathlib.Path(os.environ['GRAMSTACK_CONFIG_FILE']);config=json.loads(path.read_text());certs=path.parent/'certs'
policy=dict(registry='lokos',version='demo-policy-v1',consumer_id='consumer-one',resource='shg.membership',purpose='credit_eligibility',fields=['membership_status','savings_summary'],require_verified_subject=True,require_human_consent=True,max_grant_seconds=60,max_disclosures=1)
(path.parent/'policy.json').write_text(json.dumps(policy))
config['consent']['status_origin']=os.environ['GRAMSTACK_TEST_AUTHZ_URL']
config['authorization']=dict(key_id='demo-grant-key',listen_address=urllib.parse.urlparse(os.environ['GRAMSTACK_TEST_AUTHZ_URL']).netloc,policy_keys=[dict(registry='lokos',key_id='demo-policy-key',file=str(certs/'policy-signing.pub'))],policies=[dict(file=str(path.parent/'policy.jws'),expected_version='')],adapters=[dict(registry='lokos',audience='lokos-adapter',service_identity='lokos-adapter.gramstack.internal')],presenter_certificates={'consumer-one':str(certs/'consumer-one.pem')})
if os.environ.get('GRAMSTACK_TEST_CUSTODY')=='1':
 config['authorization']['evidence']=dict(release_profile='batch',anchoring_enabled=os.environ.get('GRAMSTACK_TEST_ANCHORING')=='1')
 config['authorization']['adapters'][0].update(adapter_id='lokos-adapter',evidence_keys=[dict(key_id='demo-adapter-key',file=str(certs/'adapter-evidence.pub'))])
if os.environ.get('GRAMSTACK_TEST_RETRIEVAL')=='1':
 config['authorization']['retrieval']=dict(max_concurrent=4)
 config['registries'][0]['discovery']=dict(version='0.1.0',origin=os.environ['GRAMSTACK_TEST_ADAPTER_PUBLIC'],resources=[dict(name='shg.membership',version='1',fields=['membership_status','savings_summary'])])
 adapter=dict(grant_issuer=config['public_origin'],grant_audience='lokos-adapter',grant_keys=[dict(key_id='demo-grant-key',file=str(certs/'grant-signing.pub'))],authority_origin=os.environ['GRAMSTACK_TEST_AUTHZ_URL'],authority_service_identity='gateway.gramstack.internal',evidence_key_id='demo-adapter-key',release_profile='batch',max_concurrent=4)
 pathlib.Path(os.environ['ADAPTER_RETRIEVAL_CONFIG_FILE']).write_text(json.dumps(adapter))
path.write_text(json.dumps(config))
PYZ
   "$scratch/gateway" sign-policy -input "$scratch/policy.json" -key "$scratch/certs/policy-signing.key" -key-id demo-policy-key -output "$scratch/policy.jws"
  fi
  start_binding
 fi
 start_gateway
 if [[ "${GRAMSTACK_TEST_BINDING:-}" == 1 ]]; then
  if [[ "${GRAMSTACK_TEST_ESIGNET:-}" == 1 ]]; then pattern='^TestActual(ESignetFederation|SubjectBinding)$'; else pattern='^TestActual(Federation|SubjectBinding)$'; fi
  if [[ "${GRAMSTACK_TEST_PORTAL:-}" == 1 ]]; then pattern="$pattern|^TestActualConsentPortal$"; fi
  gateway_go test -race -count=1 -v -run "$pattern" ./internal/authn
  GOWORK=off go -C "$root/adapters/lokos" test -modfile="$scratch/lokos.mod" -race -count=1 -v -run 'TestDurableContext|TestContextTamper|TestDurableAttempt' ./
  stop_gateway; stop_binding
  if [[ "${GRAMSTACK_TEST_WORKERS:-}" == 1 ]]; then
   python3 - <<'PYW'
import json,os,pathlib
path=pathlib.Path(os.environ['GRAMSTACK_CONFIG_FILE']);config=json.loads(path.read_text())
config['consent']['workers_enabled']=True
path.write_text(json.dumps(config))
PYW
  fi
  start_binding; start_gateway
  if [[ "${GRAMSTACK_TEST_PRODUCT:-}" == 1 ]]; then setup_product; start_product; fi
  pattern='^TestActualSubjectBindingRestart$'
  if [[ "${GRAMSTACK_TEST_WORKERS:-}" == 1 ]]; then pattern="$pattern|^TestActualConsentWorkers$"; fi
  if [[ "${GRAMSTACK_TEST_AUTHZ:-}" == 1 ]]; then pattern="$pattern|^TestActualAuthorization$"; fi
  if [[ "${GRAMSTACK_TEST_CUSTODY:-}" == 1 ]]; then pattern="$pattern|^TestActualEvidenceCustody$"; fi
  if [[ "${GRAMSTACK_TEST_RETRIEVAL:-}" == 1 ]]; then pattern="$pattern|^TestActualProtectedRetrieval$"; fi
  if [[ "${GRAMSTACK_TEST_DISCOVERY:-}" == 1 ]]; then
   pattern="$pattern|^TestActualAdapterDiscovery$"
   GOWORK=off go -C "$root/adapters/lokos" test -modfile="$scratch/lokos.mod" -race -count=1 -v -run '^TestLiveDiscoveryConformance$' .
  fi
  if [[ "${GRAMSTACK_TEST_PRODUCT:-}" == 1 ]]; then pattern="$pattern|^TestActualSakhiCreditProduct$"; fi
  if [[ "${GRAMSTACK_TEST_ANCHORING:-}" == 1 ]]; then pattern="$pattern|^TestActualEvidenceAnchoringProcess$"; fi
  gateway_go test -race -count=1 -v -run "$pattern" ./internal/authn
  if [[ "${GRAMSTACK_TEST_WORKERS:-}" == 1 ]]; then
   if [[ "${GRAMSTACK_TEST_PRODUCT:-}" == 1 ]]; then stop_product; fi
   stop_gateway
   if [[ "${GRAMSTACK_TEST_RETRIEVAL:-}" == 1 ]]; then stop_binding; start_binding; fi
   start_gateway
   if [[ "${GRAMSTACK_TEST_PRODUCT:-}" == 1 ]]; then start_product; fi
   pattern='^TestActualConsentWorkersRestart$'
   if [[ "${GRAMSTACK_TEST_AUTHZ:-}" == 1 ]]; then pattern="$pattern|^TestActualAuthorizationRestart$"; fi
   if [[ "${GRAMSTACK_TEST_CUSTODY:-}" == 1 ]]; then pattern="$pattern|^TestActualEvidenceCustodyRestart$"; fi
   if [[ "${GRAMSTACK_TEST_RETRIEVAL:-}" == 1 ]]; then pattern="$pattern|^TestActualProtectedRetrievalRestart$"; fi
   if [[ "${GRAMSTACK_TEST_PRODUCT:-}" == 1 ]]; then pattern="$pattern|^TestActualSakhiCreditProductRestart$"; fi
   if [[ "${GRAMSTACK_TEST_ANCHORING:-}" == 1 ]]; then pattern="$pattern|^TestActualEvidenceAnchoringProcess$"; fi
   gateway_go test -race -count=1 -v -run "$pattern" ./internal/authn
  fi
 else
  if [[ "${GRAMSTACK_TEST_ESIGNET:-}" == 1 ]]; then pattern='^TestActualESignetFederation$'; else pattern='^TestActualFederation$'; fi
  gateway_go test -race -count=1 -v -run "$pattern" ./internal/authn
 fi
 if [[ "${GRAMSTACK_TEST_PRODUCT_UI:-}" == 1 ]]; then
  node "$root/apps/sakhicredit/tests/browser.mjs"
  if [[ "${GRAMSTACK_TEST_ANCHORING:-}" == 1 ]]; then
   gateway_go test -race -count=1 -v -run '^TestActualEvidenceAnchoringProcess$' ./internal/authn
  fi
 fi
 if [[ "${GRAMSTACK_PREVIEW:-}" == 1 ]]; then
  # Keep the tested processes alive for the real browser; never export cookies or tokens.
  python3 - <<'PYP'
import base64,json,os,urllib.parse,urllib.request,uuid
headers={'Authorization':'Basic '+base64.b64encode(b'consumer-one:isolated-synthetic-secret').decode()}
body=urllib.parse.urlencode(dict(grant_type='client_credentials',scope='gramstack.access',audience='gramstack')).encode()
with urllib.request.urlopen(urllib.request.Request(os.environ['GRAMSTACK_TEST_HYDRA_PUBLIC']+'/oauth2/token',data=body,headers=headers)) as res: token=json.load(res)['access_token']
body=dict(registry='lokos',resource='shg.membership',subject_hint=dict(type='member_reference',value='member-12345'),fields=['membership_status','savings_summary'],purpose='credit_eligibility',access=dict(mode='single_use',valid_for_seconds=300),recipient_terms=dict(retention_policy_id='terms-v1'))
headers={'Authorization':'Bearer '+token,'Content-Type':'application/json','Idempotency-Key':'preview-'+str(uuid.uuid4())}
with urllib.request.urlopen(urllib.request.Request(os.environ['GRAMSTACK_TEST_GATEWAY_URL']+'/v1/access-requests',data=json.dumps(body).encode(),headers=headers)) as res: accepted=json.load(res)
print('Browser interaction (fictional Sunita, expires in 10 minutes): '+accepted['interaction_url'])
PYP
  echo 'Preview is running. Press Return to stop and remove the isolated runtime.'
  read -r _
 fi
 stop_gateway
fi
