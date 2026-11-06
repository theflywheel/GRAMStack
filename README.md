# GRAMStack

Read-only, consent-first interoperability for Indian DPI registries. GRAMStack coordinates federated authentication, registry-specific subject verification, human consent, scoped retrieval grants and verifiable access evidence.

- [Application SDKs](sdk/README.md) — Go, Python and JavaScript, Apache-2.0
- [Architecture site](https://docs.gramstack.try-dough.com/)
- [Implementation specifications](https://docs.gramstack.try-dough.com/specifications/)
- [Complete architecture](https://docs.gramstack.try-dough.com/complete-design)
- [Railway service URL](https://design-site-production-2e14.up.railway.app/)

## Repository layout

| Path | Contents |
|---|---|
| `apps/sakhicredit/` | Independent Go consumer product, browser ownership and encrypted result storage |
| `infra/railway/` | Project-level Railway configuration and isolated deployment tooling |
| `services/gateway/` | Independent Go core module, including Consent Manager and enforcement |
| `sdk/go/client/` | Independent Go application SDK: sign-in, consent and retrieval |
| `sdk/python/` | Python application SDK (`gramstack-client`) |
| `sdk/javascript/` | JavaScript / TypeScript application SDK (`@gramstack/client`) |
| `sdk/go/adapter/` | Independent adapter Go SDK module |
| `adapters/lokos/` | Reference adapter implemented using the SDK |
| `mocks/identity/` | Separately runnable fictional OIDC provider for demo/protocol tests |
| `specs/` | Versioned API, adapter, consent, grant, evidence and lifecycle contracts |
| `docs/` | Quartz 5 source, public architecture chapters and Railway deployment |
| `docs/content/` | Chapter Markdown and generated continuous reading document |
| `docs/scripts/` | Document assembly, export and verification |
| `docs/design/` | Original architecture artefacts |
| `.github/workflows/` | Documentation build and verification |

## Run locally

Use Node 24 and npm 10.9.2 or newer.

```sh
cd docs
npm ci
npm run build
npm run verify:content
npm run verify:server
npm start
```

The site runs on `http://localhost:8080`. Edit the numbered Markdown chapters in `docs/content/`; the build regenerates the complete document and Markdown download. See [authoring and deployment instructions](docs/README.md).

## Deployment

Railway project: `gramstack-design`; current service: `docs`. [Infrastructure configuration and onboarding examples](infra/railway/README.md) cover code-built services, upstream container images and per-service deployment settings.

```sh
npm ci --prefix infra/railway
npm run --prefix infra/railway plan
npm run --prefix infra/railway apply
railway up . --path-as-root --service docs --detach
```

The active project graph is `infra/railway/railway.ts`. Docs uses the repository-root context with `docs/Dockerfile` to include `specs/`; other services have independent sources and builds. GitHub Actions validates the repository without publishing automatically.

## Upstream

The documentation site vendors [Jacky Zhao's Quartz](https://github.com/jackyzha0/quartz) at revision `97a2d05f80c4c50534959b1d0d41cc4b3895625e`, with project-specific configuration and styles. Quartz's MIT license is retained at [docs/LICENSE.txt](docs/LICENSE.txt).

## Specifications

[Versioned specifications](specs/README.md) are the source for language-independent contracts, OpenAPI, JSON Schema, lifecycle transitions and fixtures. The docs build validates them and generates the Quartz specification section and exact downloadable copies. Run `npm run verify:specs` from `docs/` for contract checks alone.

## Go implementation

The internal [implementation task ledger](IMPLEMENTATION.md) and [gated demo checklist](DEMO-TODO.md) track the build and recorded-demo deliverables. Independent modules live in `services/gateway/`, `sdk/go/adapter/` and `adapters/lokos/`; the root `go.work` supports local development. There is no root Go module. LokOS imports and exercises the SDK. The independent [SakhiCredit consumer](apps/sakhicredit/README.md) calls the platform HTTP contract and owns its product state and encrypted received-data copy.

```sh
./scripts/check-go.sh
# Process liveness only; application readiness remains unavailable until configured.
go -C services/gateway run ./cmd/gramstack
```

Go service checks build modules explicitly with `GOWORK=off`, run race tests and vet, compare the FSM graph to the spec and launch gateway, adapter and product executables for HTTP/shutdown smoke tests. Before the SDK's first published release, the independent adapter check uses a temporary module-file replacement for the local SDK source.

DeDi uses the actual [DeDi-node publication and proof protocol](services/gateway/internal/evidence/dedi/README.md). For an isolated real-node integration test, set `DEDI_SOURCE` and `PG_BIN`, then run `scripts/test-dedi.sh`. It creates and removes a temporary local PostgreSQL cluster and publishes only synthetic commitments.
