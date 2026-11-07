---
title: Testing and operational verification
---

Different checks establish different guarantees. Run fast module/contract checks before the full provider and browser suites.

| Check | Command or source | What it exercises |
|---|---|---|
| Go modules | `./scripts/check-go.sh` | Race tests, vet, module builds and process smoke checks |
| Application SDKs | `SDK_PYTHON=python ./scripts/test-client-sdks.sh` | Shared isolated TLS/mTLS and signed OIDC fixtures across Go, Python and JavaScript |
| PostgreSQL | `PG_BIN=… ./scripts/test-postgres.sh` | Real SQL migrations, concurrency and durable state |
| Hydra and browser | `PG_BIN=… HYDRA_BIN=… GRAMSTACK_TEST_PRODUCT_UI=1 ./scripts/test-hydra.sh` | Actual Hydra, sharing flows, recovery and standalone SDK sign-in |
| Official eSignet images | `PG_BIN=… HYDRA_BIN=… ./infra/railway/check-esignet-images.sh` | Staged eSignet deployment, provider restart and product browser acceptance |
| DeDi | `DEDI_SOURCE=… PG_BIN=… ./scripts/test-dedi.sh` | Actual node publication and proof verification |
| Docs | `npm run build && npm run verify:content && npm run verify:server` from `docs/` | Contracts, diagrams, links, redirects and serving behavior |

Use the repository's pinned provider versions and the required runtimes. Test runners create isolated fictional state and remove their temporary processes/databases; consult their documented environment requirements before running.

## Validate an approval use

On an operator-controlled gateway runtime, `gramstack verify-admission <request-id>` compares retained admission/evidence records for that request. This is an operator check, not a public data or management endpoint.

Compare the consent-history card's approval-use time, response outcome and receipt reference. DeDi verification is asynchronous and has its own result. An absent completed disclosure and an uncertain release are distinct outcomes.

[Detailed platform audit procedure](/architecture/08-state-and-evidence#validate-one-disclosure-at-the-platform-layer) · [CI workflows](https://github.com/theflywheel/GRAMStack/tree/main/.github/workflows)
