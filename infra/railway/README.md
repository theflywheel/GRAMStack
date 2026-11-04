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
