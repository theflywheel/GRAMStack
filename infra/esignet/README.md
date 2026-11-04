# Local eSignet demo stack

Official eSignet 2.0.0 service and UI, official mock identity system 0.14.0, PostgreSQL and Redis. Images are pinned by digest. All published ports bind to `127.0.0.1`; this configuration uses public demo passwords and software keystores. It is for isolated local development. Railway requires separate secrets, routing and ingress restrictions before deployment.

```sh
open -a Docker
docker compose -p gramstack-esignet -f infra/esignet/compose.yaml up -d
python3 infra/esignet/check.py
```

The UI is at http://127.0.0.1:19300 and the issuer is http://127.0.0.1:19080. First startup may take a minute. Client onboarding and fictional identity seeding are separate integration steps; a healthy discovery document does not demonstrate gateway login. The mock source is not government authentication.

Postgres, Redis and **both** service keystores use named volumes. Recreate containers without deleting those volumes. Database key references and their keystore must be backed up/restored together. `down` retains volumes; `down -v` destroys this demo's state. The init SQL runs only on a fresh database volume. It creates dedicated demo databases and must never run against an existing shared database.

If Docker Desktop's credential helper waits on a locked macOS keychain, public image pulls can use an isolated Docker config with empty `auths`. Do not change the user's existing Docker credentials. Use that same config for all commands in the session.

## Fresh isolated identity instance

For a demo alongside an existing eSignet project, generate a new project name,
four loopback ports and distinct database and software keystore passwords.
Keep this private state directory with its Compose volumes for restarts.

```sh
state=$(mktemp -d)
python3 infra/esignet/init-demo.py "$state/esignet"
set -a
. "$state/esignet/.env"
set +a
docker compose -f infra/esignet/compose.yaml up -d --wait --wait-timeout 240
python3 infra/esignet/check.py
```

Use the same exported variables when running the federation or browser checks
below. The project name isolates volumes and the published ports stay on
127.0.0.1. Start Compose promptly after generating ports. If another process
takes a port, Compose reports the conflict and a new state directory can be
generated. Use the same configuration for a restart. Password changes require
an explicit database and keystore rotation.

`docker compose -f infra/esignet/compose.yaml down` stops this project and
keeps its volumes. Run it with the same exported project configuration.

## Single-origin ingress verification

Use a new isolated project as above and set
`ESIGNET_DEMO_ISSUER_PORT="$ESIGNET_DEMO_UI_PORT"`. This makes the browser
ingress the issuer while keeping fixture provisioning on the separate
loopback API port. Build and launch the same images prepared for Railway:

```sh
export ESIGNET_DEMO_ISSUER_PORT="$ESIGNET_DEMO_UI_PORT"
docker compose -f infra/esignet/compose.yaml -f infra/esignet/ingress-test.compose.yaml up -d --build --wait
python3 infra/esignet/check.py
GRAMSTACK_TEST_ESIGNET=1 GRAMSTACK_TEST_PRODUCT_UI=1 \
  PG_BIN=/path/to/postgres/bin HYDRA_BIN=/path/to/hydra ./scripts/test-hydra.sh
```

The override removes the source configuration bind mounts so the test runs
the baked SQL, demo flow and ingress configuration. Use a fresh project each
time. The ingress forwards the demo's OIDC routes and the three UI flow
operations. Client management, identity provisioning, PAR, introspection and
unsupported API routes are inaccessible through the UI listener. The API
and mock identity ports in this local check remain bound to loopback.

## Provenance

SQL and nginx configuration originate from [MOSIP eSignet v2.0.0](https://github.com/mosip/esignet/tree/db528423dac7898ad546cb7b2f5f867a949676a7/docker-compose), commit `db528423dac7898ad546cb7b2f5f867a949676a7`, under MPL-2.0 (included). The upstream personal-looking example identity was omitted from SQL. Compose adds Redis, persistent key files, dependency health checks, loopback ports and immutable image references. Upstream documentation: https://docs.esignet.io/.

Provider integration must support eSignet's advertised `private_key_jwt` token authentication and `PS256` ID tokens. Keep this in the auth-provider adapter/profile; Hydra and request/session enforcement remain shared platform behavior. See D03a in the demo checklist for the login acceptance gate.

## Full gateway federation check

With the stack running, use the repository runner with PostgreSQL and the official Hydra v26.2.0 binary:

```sh
GRAMSTACK_TEST_ESIGNET=1 PG_BIN=/path/to/postgres/bin HYDRA_BIN=/path/to/hydra ./scripts/test-hydra.sh
```

It creates isolated gateway/Hydra SQL databases, onboards a new client into the local eSignet instance, seeds fictional Sunita/Rani records, keeps its client key in a private temporary directory, and tests the complete HTTP federation and denial paths. Each run adds an ephemeral client to this disposable eSignet database; the private key and temporary SQL instance are cleaned up. The browser UI rendering and final recorded demo have separate acceptance checks. Public local fixture OTP is `111111`; the identity service is mock, not a government connection.

## Local browser flow

`flow-local-demo.yaml` is the pinned upstream authentication flow with CAPTCHA components, required CAPTCHA inputs and the CAPTCHA interceptor removed. This matches this isolated stack's already unconfigured CAPTCHA validator and keeps the unmodified official UI usable with fictional identities. OTP verification and its attempt limit remain in place. This is **not a CAPTCHA implementation or a production authentication flow**. No CAPTCHA challenge is solved or forwarded. Production identity deployments must supply their approved flow and working CAPTCHA provider/validator; do not mount this file there.

To keep the tested gateway, Hydra, SDK adapter and native mock online for browser review:

```sh
GRAMSTACK_TEST_PORTAL=1 GRAMSTACK_TEST_ESIGNET=1 GRAMSTACK_PREVIEW=1 PG_BIN=/path/to/postgres/bin HYDRA_BIN=/path/to/hydra ./scripts/test-hydra.sh
```

After the tests and restart checks pass, open the printed interaction URL, select UIN/VID and use fictional Sunita ID `9000000001` with mock OTP `111111`. The interaction expires after ten minutes. Press Return in the runner to stop it and remove its isolated state. The preview does not activate consent until the durable worker is configured; an approval receipt reports that distinction. This runner is a development acceptance tool, not the final reproducible product stack.

For the complete consent-stage browser flow, replace `GRAMSTACK_TEST_PORTAL=1` above with `GRAMSTACK_TEST_WORKERS=1`. This includes the same portal checks, then starts durable workers after restart acceptance. Approval first records the choice; use Refresh status to observe verified activation. Withdrawal stops future admission while retaining the signed artefact. No registry business data is retrieved at this stage.
