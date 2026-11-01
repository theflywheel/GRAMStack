# GRAMStack

Read-only, consent-first interoperability for Indian DPI registries. GRAMStack coordinates federated authentication, registry-specific subject verification, human consent, scoped retrieval grants and verifiable access evidence.

- [Architecture site](https://docs.gramstack.try-dough.com/)
- [Implementation specifications](https://docs.gramstack.try-dough.com/specifications/)
- [Complete architecture](https://docs.gramstack.try-dough.com/complete-design)
- [Railway service URL](https://design-site-production-2e14.up.railway.app/)

## Repository layout

| Path | Contents |
|---|---|
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

Railway project: `gramstack-design`; service: `docs`. Run `railway up . --path-as-root --service docs --detach` from the repository root after authenticating and linking the CLI. For a Git-based deployment, use repository root `/` with `docs/Dockerfile`. The repository-root build context includes `specs/`; the app remains in `docs/`. The Dockerfile builds Quartz and serves only generated static output through a dependency-free server.

GitHub Actions validates the documentation; it does not publish automatically.

## Upstream

The documentation site vendors [Jacky Zhao's Quartz](https://github.com/jackyzha0/quartz) at revision `97a2d05f80c4c50534959b1d0d41cc4b3895625e`, with project-specific configuration and styles. Quartz's MIT license is retained at [docs/LICENSE.txt](docs/LICENSE.txt).

## Specifications

[Versioned specifications](specs/README.md) are the source for language-independent contracts, OpenAPI, JSON Schema, lifecycle transitions and fixtures. The docs build validates them and generates the Quartz specification section and exact downloadable copies. Run `npm run verify:specs` from `docs/` for contract checks alone.

The Railway service must use Dockerfile path `docs/Dockerfile`, repository-root build context and health check `/healthz`; these service settings match the root `railway.toml`.
