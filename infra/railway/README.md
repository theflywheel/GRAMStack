# Railway deployment for the monorepo

The active project definition is [railway.ts](railway.ts). It describes every active service in Railway project `gramstack-design`, production environment. Each service has its own source, build settings, watch paths, health check, variables and networking. There is no root `railway.toml` that turns every service into the docs app.

The Go backend remains Go. This small, isolated Node toolchain authors Railway infrastructure using its supported TypeScript SDK; it is not an application workspace or a backend runtime dependency. The SDK is pinned in this directory. Use Railway CLI 5.71.0 or newer (validated with 5.71.0), installed from Railway or Homebrew; the legacy 4.x CLI cannot manage this configuration.

## Current service

| Service | Source/deployment | Build context | Build | Public endpoint |
|---|---|---|---|---|
| `docs` | Targeted local CLI upload | Repository root | `docs/Dockerfile` | `https://docs.gramstack.try-dough.com/` |

Docs needs both `docs/` and `specs/`, so its context stays at the repository root. Other services choose their own context. The existing generated Railway URL and private hostname are preserved during import. GitHub autodeploy is not enabled by this change.

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

An infrastructure apply configures services; source deployment is a separate operation. Deploy docs from the repository root:

```sh
railway up . --path-as-root --service docs --detach
```

Target `--service` on every upload, log and redeploy command. Watch paths isolate Git-triggered builds; they do not filter an explicitly requested CLI deployment. `.railwayignore` filters CLI uploads; `.dockerignore` filters repository-root Docker contexts. A Dockerfile located in a subdirectory still uses COPY paths relative to its selected build context.

## Adding a code-built service

Add another `service(...)` to the same definition and include it in `resources`. Choose either Railpack build/start commands or a service-specific Dockerfile. For the Go backend, after its code and Dockerfile exist, a GitHub-connected service can use:

```ts
import { github, service } from "railway/iac";

const gramstack = service("gramstack", {
  source: github("theflywheel/GRAMStack", { branch: "main", rootDirectory: "/" }),
  build: {
    builder: "DOCKERFILE",
    dockerfilePath: "infra/containers/gramstack/Dockerfile",
    watchPatterns: [
      "/cmd/gramstack/**", "/internal/**", "/go.mod", "/go.sum",
      "/specs/**", "/infra/containers/gramstack/**", "/.dockerignore",
    ],
  },
  healthcheck: "/healthz",
  deploy: { restartPolicyMaxRetries: 3 },
});
```

This is an onboarding example, not an active service or an assertion that the backend exists. The code service can instead omit `source` and use targeted CLI uploads, as docs does today. Connecting a GitHub source enables the repository deployment path; configure its branch and checks deliberately. For an independent component with no shared files, use its subdirectory as root and make all build commands/Dockerfile COPY paths relative to that directory. Mocks remain under `mocks/`, separate from core `cmd/` and production services.

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
