---
title: Implemented API catalogue
description: Versioned GRAMStack implementation contracts and conformance requirements.
---

# Implemented HTTP API catalogue 0.1.0

These OpenAPI 3.1 documents describe the configured reference runtime and accompany
core 0.3.0 and its independently versioned profiles. The immutable protocol remains
in `specs/0.3.0`. Runtime documents cover public gateway and browser routes,
private gateway mTLS routes, SDK-backed adapter routes and the SakhiCredit product API.

The portal uses URL-encoded forms and server redirects. Internal consent management
runs in-process. The protocol's separate consent-service RPC routes are therefore
absent from the deployed HTTP catalogue. Health and static assets are operational
endpoints outside this business API catalogue. Hydra and eSignet expose their
upstream OAuth/OIDC interfaces.

Browse the same documents at [API explorer](https://docs.gramstack.try-dough.com/api/).
The published explorer has request execution disabled. Use the product for browser
flows and an approved operator environment for private mTLS calls.

Update these files alongside handler changes. The docs build validates OpenAPI,
bundles references locally and checks the documented operations against route sources.
