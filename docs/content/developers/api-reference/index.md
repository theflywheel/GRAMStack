---
title: API reference
---

Swagger is hosted with each running API service. The documentation site links to those explorers and publishes the versioned protocol sources.

| Service | Explorer | Scope |
|---|---|---|
| Gateway | [Open Swagger](https://gramstack-gateway-b9b69819.up.railway.app/swagger/) | Public/browser, private mTLS and core protocol catalogues |
| LokOS adapter | [Open Swagger](https://gramstack-adapter-b9b69819.up.railway.app/swagger/) | Adapter verification and retrieval contracts |
| SakhiCredit | [Open Swagger](https://sakhicredit-b9b69819.up.railway.app/swagger/) | Product APIs and SDK-backed sign-in |

The explorers describe authentication requirements; hosting an explorer does not make private operations publicly callable. Use the registered credentials and approved service network for protected operations.

[Implemented route catalogue](/developers/api-reference/catalogue) · [Core OpenAPI source](/developers/specifications/schemas/openapi) · [Architecture API discussion](/architecture/07-api-contracts)
