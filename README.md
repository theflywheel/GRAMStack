# GRAMStack

Read-only, consent-first interoperability for Indian DPI registries. GRAMStack coordinates federated authentication, registry-specific subject verification, human consent, scoped retrieval grants and verifiable access evidence.

- [Architecture site](https://docs.gramstack.try-dough.com/)
- [Complete architecture](https://docs.gramstack.try-dough.com/complete-design)
- [Railway service URL](https://design-site-production-2e14.up.railway.app/)

## Repository layout

| Path | Contents |
|---|---|
| `design-site/` | Quartz 5 source, public architecture chapters and Railway deployment |
| `design-site/content/` | Chapter Markdown and generated continuous reading document |
| `design-site/scripts/` | Document assembly, export and verification |
| `docs/design/` | Original architecture artefacts |
| `.github/workflows/` | Documentation build and verification |

## Run locally

Use Node 24 and npm 10.9.2 or newer.

```sh
cd design-site
npm ci
npm run build
npm run verify:content
npm run verify:server
npm start
```

The site runs on `http://localhost:8080`. Edit the numbered Markdown chapters in `design-site/content/`; the build regenerates the complete document and Markdown download. See [authoring and deployment instructions](design-site/README.md).

## Deployment

Railway project: `gramstack-design`; service: `design-site`. Run `railway up --service design-site --detach` from `design-site/` after authenticating and linking the CLI. For a Git-based deployment, set the Railway service root directory to `/design-site`. The Dockerfile builds Quartz and serves only generated static output through a dependency-free server.

GitHub Actions validates the documentation; it does not publish automatically.

## Upstream

The documentation site vendors [Jacky Zhao's Quartz](https://github.com/jackyzha0/quartz) at revision `97a2d05f80c4c50534959b1d0d41cc4b3895625e`, with project-specific configuration and styles. Quartz's MIT license is retained at [design-site/LICENSE.txt](design-site/LICENSE.txt).
