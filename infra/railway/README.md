# Railway deployment for the monorepo

The active project definition is [railway.ts](railway.ts). It describes every active service in Railway project `gramstack-design`, production environment. Each service has its own source, build settings, watch paths, health check, variables and networking. There is no root `railway.toml` that turns every service into the docs app.

The Go backend remains Go. This small, isolated Node toolchain authors Railway infrastructure using its supported TypeScript SDK; it is not an application workspace or a backend runtime dependency. The SDK is pinned in this directory. Use Railway CLI 5.71.0 or newer (validated with 5.71.0), installed from Railway or Homebrew; the legacy 4.x CLI cannot manage this configuration.

## Current services

| Service | Source/deployment | Build context | Build | Public endpoint |
|---|---|---|---|---|
| `docs` | Targeted local CLI upload | Repository root | `docs/Dockerfile` | `https://docs.gramstack.try-dough.com/` |
| `platform-db` | Targeted local CLI upload | Repository root | `infra/railway/database/Dockerfile` | Private Railway network |

| `hydra-admin` | Pinned official Hydra image | — | `serve admin` | Private TLS on 4445 |
| `hydra-public` | Same pinned Hydra image | — | `serve public` | `https://gramstack-hydra-b9b69819.up.railway.app/` |
| `dedi` | Targeted local CLI upload | Repository root | `infra/railway/dedi/Dockerfile` | Private TLS on 8446 |

The platform database uses a pinned PostgreSQL 17 image with a persistent
`platform-data` volume at `/var/lib/postgresql/data`. PostgreSQL initializes
in its `pgdata` subdirectory. The initial deployment
requires `POSTGRES_PASSWORD` and separate `GRAMSTACK_DB_<ROLE>_PASSWORD`
secrets for `CORE`, `ADAPTER`, `PRODUCT`, `HYDRA` and `DEDI`. Each role
owns one database and is denied connections to its peers. Applications use
their own credential over the private network. The bootstrap superuser is
reserved for the database. PostgreSQL initialization runs only on an empty
volume. Rotate credentials in the database and corresponding applications
together.

Docs needs both `docs/` and `specs/`, so its context stays at the repository root. Other services choose their own context. The existing generated Railway URL and private hostname are preserved during import. GitHub autodeploy is not enabled by this change.

## DeDi evidence node

The private `dedi` service builds the pinned upstream node from its verified
source archive. Set `DEDI_DB_URL` to the dedicated `dedi` database, with
`DEDI_ADMIN_PASSWORD`, `DEDI_PUBLISHER_KEYS` and `DEDI_ORIGIN` set in Railway.
Generate a stable node signing key with `dedid keygen` and put it in the
runtime bundle as `node.key`. Set `DEDI_KEY_FILE=/run/gramstack/node.key`.
The bundle also contains `dedi.pem` and `dedi.key`, the TLS identity for
`dedi.railway.internal`.

The TLS listener on 8446 forwards to the node's loopback listener. The HTTP
probe exposes only `GET /healthz`. Neither listener has a public domain or TCP
proxy. The gateway uses `https://dedi.railway.internal:8446` and a trust bundle
that includes the DeDi CA. Provision the commitment registry explicitly
with `gramstack anchor -provision` before starting the gateway worker. The
gateway also needs its publisher signing key and the node's checkpoint
verifier. Keep both the node identity and database stable across redeploys.

## Hydra deployment

Both roles use the same `DSN` for the `hydra` database and the same random
`SECRETS_SYSTEM`. Set the public HTTPS issuer in `URLS_SELF_ISSUER` and the
gateway HTTPS login and consent routes in `URLS_LOGIN` and `URLS_CONSENT`.
Keep credentials in Railway variables and set the same values on both roles.

Deploy admin first. Its pre-deploy command runs Hydra's explicit SQL migration.
Set `SERVE_ADMIN_PORT=4445`, `SERVE_TLS_ENABLED=true` and supply standard padded
base64 PEM values in `SERVE_TLS_CERT_BASE64` and `SERVE_TLS_KEY_BASE64`.
Use a private CA certificate for `hydra-admin.railway.internal` and include that
CA in the gateway's TLS trust bundle. The admin role has no public domain or TCP
proxy. The public role sets `PORT=4444`, `SERVE_PUBLIC_PORT=4444` and
`SERVE_TLS_ENABLED=false` behind Railway HTTPS ingress. Use its HTTPS origin
for discovery and token requests. Neither role uses Hydra development mode.

Reproduce the listener split locally with
`PG_BIN=... HYDRA_BIN=... ./infra/railway/check-hydra-split.sh`. The probe uses
fresh PostgreSQL state and verifies private TLS, shared OAuth client and token
state, and a 404 for the admin route on the public listener.

Prepare the two client secrets in private operator state. Run registration
from an operator host with private-network access to Hydra admin:

```sh
state=$(mktemp -d /tmp/gramstack-hydra-clients.XXXXXX)
python3 infra/railway/provision-hydra.py prepare "$state/clients.json"
python3 infra/railway/provision-hydra.py register "$state/clients.json" --ca /private/hydra-ca.pem
```

The registration command uses the fixed private TLS endpoint and refuses
redirects and existing client IDs. Keep the file if a request fails or
only one client is created. Inspect the admin state before retrying so
an existing client is not replaced or assigned a new secret. The gateway
uses the portal secret and SakhiCredit uses the consumer secret.

## eSignet deployment artifacts

The `esignet-db`, `esignet` and `esignet-ui` build contexts are prepared under
`infra/railway/`. Start the dedicated PostgreSQL and Redis dependencies, then
the official mock identity service and the private API. The API carries the
same demo-only flow used in the local eSignet test. Preserve the SQL and
software keystore volumes across redeploys.

The public UI image forwards its allowlisted login and OIDC routes to
`esignet.railway.internal:8080`. Configure the public UI's HTTPS origin as the
issuer and the UI redirect origin. Keep the API, client onboarding, mock
identity management, PostgreSQL and Redis on the private network. See the
[local single-origin check](../esignet/README.md#single-origin-ingress-verification)
for the exact baked-image verification. The Railway service graph reserves the four private volumes and a single
public ingress. The services are created before image activation so their
runtime variables can be set first. Prepare private operator settings with:

```sh
state=$(mktemp -d /tmp/gramstack-esignet-railway.XXXXXX)
python3 infra/railway/prepare-esignet.py "$state/settings"
```

Keep the generated files outside the repository. Deploy database and Redis
first, then the official mock identity image pinned in the local Compose
file, the private eSignet API, and the UI. Use the same credentials with the
same volumes on restart. Hosted client registration and fictional identity
fixture provisioning remain separate operator steps.

## Application routing

The gateway, adapter metadata and SakhiCredit UI each have a dedicated HTTPS
origin targeting port 8080. Keep the gateway data listener, adapter RPCs and
fictional LokOS source on private DNS with the existing mTLS identities.

| Service | Public endpoint | Private role |
|---|---|---|
| Gateway | `https://gramstack-gateway-b9b69819.up.railway.app` | Authenticated data and custody listener |
| LokOS adapter | `https://gramstack-adapter-b9b69819.up.railway.app` | Subject verification and retrieval |
| Mock LokOS | None | Fictional native source |
| SakhiCredit | `https://sakhicredit-b9b69819.up.railway.app` | Uses the gateway data listener as a consumer |

Generate the demo mTLS identities in a fresh private directory:

```sh
state=$(mktemp -d /tmp/gramstack-railway-mtls.XXXXXX)
GOWORK=off go run ./tools/devcerts/main.go -railway-demo -dir "$state/certs"
```

The gateway, adapter and source certificates include their Railway private
DNS names and retain the existing `.gramstack.internal` service identities
for peer authorization. The consumer certificate retains its approved
consumer identity. The hosted demo certificates expire after 30 days.
Keep private keys in their assigned service bundles and distribute the
CA certificate to the authorized peers.

Use `https://gateway.railway.internal:8445` for data, redemption and custody
with the gateway private listener bound to `:8445`.
Use `https://lokos-adapter.railway.internal:8443` for adapter RPCs and
`https://mock-lokos.railway.internal:8444` for the adapter's native source.
Set the gateway and source modes to `demo` and keep the eSignet demo provider
and registry verifier explicitly enabled. Keep loopback HTTP disabled.
The gateway's discovery configuration uses the public adapter metadata URL.

After private eSignet client onboarding, generate the runtime JSON from the
onboarded provider profile:

```sh
python3 infra/railway/prepare-app-config.py --provider "$state/esignet-profile.json" "$state/config"
go run ./services/gateway/cmd/gramstack sign-policy \
  -input "$state/config/policy.json" -key "$state/certs/policy-signing.key" \
  -key-id demo-policy-key -output "$state/config/policy.jws"
```

The generator pins the public issuers, private listener names and approved
demo policy. Put `gateway.json`, `adapter.json` and `product.json` in their
respective service bundles. The gateway also needs the signed `policy.jws`.
Files named by the configurations resolve under `/run/gramstack`.
Private eSignet onboarding still needs to produce the provider profile,
client assertion key and fictional subject bindings.

Set `GRAMSTACK_PRIVATE_CA_FILE` to the gateway bundle file containing the
private Hydra admin and DeDi CA certificates. The gateway image appends those
roots to its system roots at startup so it can still verify the public
Hydra issuer, eSignet issuer and adapter metadata certificates. This is
separate from the mTLS CA configured for private GRAMStack data routes.

The three SQL-backed applications run their explicit migration commands
before deployment. Configure dependencies and service variables before
uploading the committed images. The service placeholders and domain
reservation alone do not imply an active application.

## Tooling and commands

Use Node 24 and install or upgrade the CLI with `brew install railway` / `brew upgrade railway`. From the repository root:

```sh
npm ci --prefix infra/railway
npm run --prefix infra/railway check
npm run --prefix infra/railway plan
npm run --prefix infra/railway apply
```

The plan/apply scripts select production explicitly and use `--file railway.ts` relative to this directory. Railway's CLI finds the existing project linkage in the parent directory. To link a fresh checkout, use the CLI:

```sh
railway link --project gramstack-design --environment production
```

`plan` is read-only. Review its changes, then `apply` updates the selected environment. Keep all active resources in the `resources` list: removing a resource declares its removal. Never use a docs-only partial file to apply over an environment that already contains other services.

An infrastructure apply configures services; source deployment is a separate operation. Stage docs from a committed revision and upload its dedicated build context:

```sh
stage=$(./infra/railway/stage.sh docs HEAD)
railway up "$stage" --path-as-root --service docs --environment production --detach
```

The staging command selects the service's manifest from `deploy/`, places
it at the upload root as `railway.json`, and includes only committed build
inputs for that role. The repository root has no shared Railway manifest.
Use the same command with `platform-db` for the private database. The Go
service manifests are prepared for the hosted rollout. Their pre-deploy
commands apply explicit migrations using the runtime file entrypoint.

Target `--service` on every upload, log and redeploy command. Watch paths isolate Git-triggered builds; they do not filter an explicitly requested CLI deployment. `.railwayignore` filters CLI uploads; `.dockerignore` filters repository-root Docker contexts. A Dockerfile located in a subdirectory still uses COPY paths relative to its selected build context.

## Adding a code-built service

Add another `service(...)` to the same definition and include it in `resources`. Choose either Railpack build/start commands or a service-specific Dockerfile. For the Go backend, after its code and Dockerfile exist, a GitHub-connected service can use:

```ts
import { github, service } from "railway/iac";

const gateway = service("gateway", {
  source: github("theflywheel/GRAMStack", { branch: "main", rootDirectory: "/" }),
  build: {
    builder: "DOCKERFILE",
    dockerfilePath: "infra/containers/gateway/Dockerfile",
    watchPatterns: [
      "/services/gateway/**",
      "/specs/**", "/infra/containers/gateway/**", "/.dockerignore",
    ],
  },
  healthcheck: "/healthz",
  deploy: { restartPolicyMaxRetries: 3 },
});
```

This is an onboarding example, not an active Railway service; the gateway module exists under `services/gateway/`, while its application routes and deployment image are still being built. The code service can instead omit `source` and use targeted CLI uploads, as docs does today. Connecting a GitHub source enables the repository deployment path; configure its branch and checks deliberately. For an independent component with no shared files, use its subdirectory as root and make all build commands/Dockerfile COPY paths relative to that directory. Mocks remain under `mocks/`, separate from core `cmd/` and production services.

## Adding an upstream image service

A dependency distributed as a ready-made image does not need a dummy source folder or a Dockerfile. Use Railway's `image` source with a reviewed release image pinned by digest:

```ts
import { image, service } from "railway/iac";

const esignet = service("esignet", {
  source: image("<verified-eSignet-image>@sha256:<reviewed-digest>", {
    autoUpdates: { type: "disabled" },
  }),
  deploy: { restartPolicyMaxRetries: 3 },
});
```

Replace the image placeholders and set the health path, port, variables and dependencies from the selected upstream release before adding this service to `resources`. This example is not deployed by the active definition. An image service does not inherit docs' Dockerfile or watch paths. If an upstream service needs bundled configuration, put a thin Dockerfile and configuration under `infra/containers/<service>/` and configure that service's own image build instead.

[eSignet's deployment documentation](https://github.com/mosip/esignet/tree/master/docker-compose) defines its release-specific application, UI and supporting services. Model each required long-running container as a separate Railway service and database/volume as its own resource. A Docker Compose file is a useful topology reference, not a process to run inside one Railway container. Configure the identity-provider public issuer/callback URLs according to that release; a private hostname alone is not a browser-reachable issuer.

## Variables, networking and environments

Variables are explicitly **unmanaged by IaC** in the current definition. Keep secrets and service-reference variables in Railway; applies never export, reset or delete them. Use Railway service references for connections and private DNS for service-to-service traffic. Publish only the endpoints needed by users or protocol callbacks. Do not copy service credentials into repository files or Docker layers.

Before adding staging, make domain and replica choices environment-specific in the one project definition. The current scripts intentionally target production. Run an explicit plan for a new environment before applying it; never reuse the production custom domain there.

## References

- [Railway infrastructure as code](https://docs.railway.com/infrastructure-as-code)
- [TypeScript service/source reference](https://docs.railway.com/infrastructure-as-code/reference)
- [Monorepo roots and watch paths](https://docs.railway.com/deployments/monorepo)

The older per-deployment `railway.toml` format is deprecated by Railway. New services belong in this project graph; they do not need copies of the old global docs configuration.
