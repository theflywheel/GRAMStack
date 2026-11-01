# GRAMStack design site

Source: [theflywheel/GRAMStack](https://github.com/theflywheel/GRAMStack).

Public architecture documentation built on [Quartz 5](https://github.com/jackyzha0/quartz).

- Node 24 / npm 10.9.2 or newer.
- `npm ci`
- `npm run build`
- `npm run verify:content`
- `npm start` (serves `public/` on `0.0.0.0:$PORT`, default 8080).

## Authoring

Edit `content/01-*.md` through `content/12-*.md`. Run `npm run assemble` to regenerate the continuous reading page, then build. The build emits a downloadable Markdown document. Original design artefacts and internal review notes are retained locally under `design/` and `internal/`. They are excluded from publication.

The public site presents the architecture directly. Illustrative payloads and technical security requirements remain explicit. Internal approval and delivery tracking are maintained separately. Raw private transcripts, credentials and team-chat attachments are not published.

## Railway

Project: `gramstack-design`; service: `docs`.

The multi-stage Dockerfile builds Quartz and runs a dependency-free static server. Only the static output and server enter the runtime image. It does not run GRAMStack, connect registries or store identities. Railway health check: `/healthz`, port 8080.

From the repository root, with Railway authenticated and linked:

```sh
railway up . --path-as-root --service docs --detach
railway logs --build
```

The repository root is the Docker build context so the build can include the canonical sibling `specs/` directory. The application and Markdown remain under `docs/`. `railway.toml`, `.railwayignore` and `.dockerignore` control packaging. Set the public host in `quartz.config.yaml` before changing domains. The source repository is `theflywheel/GRAMStack`, with this site under `docs/`. Railway deployments upload the repository root through the CLI; GitHub CI validates the build without deploying.

The deployed URL is public to anyone who has it. Robots metadata requests no indexing; that is not access control. The content contains design material and fictional examples, not private transcript dumps.

## Verification and provenance

`npm run verify:content` checks generated local links/assets, titles, JSON examples, export completeness and accidental local transcript/auth references. Browser QA checks rendered diagrams, search, navigation and responsive layout. Server tests cover clean URLs, 404, HEAD, health and method/path handling.

Upstream Quartz revision at creation: `97a2d05f80c4c50534959b1d0d41cc4b3895625e`. Retain upstream MIT licensing. The lockfile pins npm dependencies. The configured plugins are npm-backed; no separate Git plugin checkout is required.

Dependency audit after non-breaking fixes: two moderate advisories remain in upstream build/development glob-serving dependencies (`minimatch` / `serve-handler`). They are not copied into the dependency-free production runtime image. Avoid running Quartz's development server with untrusted inputs; review upstream fixes when upgrading. High-severity advisories found on the initial install were resolved with non-breaking updates.

## Versioned specifications

Edit `../specs/` for normative contracts. `npm run verify:specs` checks the OpenAPI document, schema references, fixtures and lifecycle. The build generates `content/specifications/` from those sources and copies exact downloads to `public/static/specs/`. Generated Markdown is committed alongside the architecture export; public build output is ignored by Git. Do not edit generated specification pages.

The Railway service must use Dockerfile path `docs/Dockerfile`, repository-root build context and health check `/healthz`; these service settings match the root `railway.toml`.
