# GRAMStack design site

Source: [theflywheel/GRAMStack](https://github.com/theflywheel/GRAMStack).

Public architecture documentation built on [Quartz 5](https://github.com/jackyzha0/quartz).

- Node 24 / npm 10.9.2 or newer.
- `npm ci`
- `npm run build`
- `npm run verify:content`
- `npm start` (serves `public/` on `0.0.0.0:$PORT`, default 8080).

## Authoring

Edit `content/01-*.md` through `content/12-*.md`. Run `npm run assemble` to regenerate the continuous reading page, then build. The build emits a downloadable Markdown document. Original design artefacts and internal review notes remain outside this project under `../docs/design/` and `../docs/internal/`. They are excluded from publication.

The public site presents the architecture directly. Illustrative payloads and technical security requirements remain explicit. Internal approval and delivery tracking are maintained separately. Raw private transcripts, credentials and team-chat attachments are not published.

## Railway

Project: `gramstack-design`; service: `design-site`.

The multi-stage Dockerfile builds Quartz and runs a dependency-free static server. Only the static output and server enter the runtime image. It does not run GRAMStack, connect registries or store identities. Railway health check: `/healthz`, port 8080.

From this directory, with Railway authenticated and linked:

```sh
railway up --service design-site --detach
railway logs --build
```

The source folder is the deployment root. `railway.toml`, `.railwayignore` and `.dockerignore` control packaging. Set the public host in `quartz.config.yaml` before changing domains. The source repository is `theflywheel/GRAMStack`, with this site under `design-site/`. Railway deployments upload this directory through the CLI; GitHub CI validates the build without deploying.

The deployed URL is public to anyone who has it. Robots metadata requests no indexing; that is not access control. The content contains design material and fictional examples, not private transcript dumps.

## Verification and provenance

`npm run verify:content` checks generated local links/assets, titles, JSON examples, export completeness and accidental local transcript/auth references. Browser QA checks rendered diagrams, search, navigation and responsive layout. Server tests cover clean URLs, 404, HEAD, health and method/path handling.

Upstream Quartz revision at creation: `97a2d05f80c4c50534959b1d0d41cc4b3895625e`. Retain upstream MIT licensing. The lockfile pins npm dependencies. The configured plugins are npm-backed; no separate Git plugin checkout is required.

Dependency audit after non-breaking fixes: two moderate advisories remain in upstream build/development glob-serving dependencies (`minimatch` / `serve-handler`). They are not copied into the dependency-free production runtime image. Avoid running Quartz's development server with untrusted inputs; review upstream fixes when upgrading. High-severity advisories found on the initial install were resolved with non-breaking updates.
