# Fictional native registry source

This independent mock service is not the LokOS API. It owns two fictional membership/savings fixtures and an explicit issuer/subject-to-record map. It imports neither the gateway nor the production adapter.

Startup requires `GRAMSTACK_ENV=development` or `demo`, `OIDC_ISSUER`, `OIDC_SUBJECT_BINDINGS_FILE`, and `SERVICE_CA_FILE`/`SERVICE_CERT_FILE`/`SERVICE_KEY_FILE`. The private JSON binding file maps `member-12345` and/or `member-67890` to their configured provider subjects. There is no default identity mapping or public provisioning endpoint. Constructor configuration is copied so caller mutation cannot change source policy. The eSignet demo tool provisions actual verified pairwise subjects for fictional Sunita/Rani accounts; the fast test IdP uses explicit test subjects.

The service requires mTLS and the `lokos-adapter.gramstack.internal` certificate identity on every route, including health. POST `/native/v1/verify-subject` checks issuer, person, locator and authentication freshness, then returns only an opaque mapping handle marked `demo: true`. GET `/native/v1/members/{handle}` returns the fictional row only to the adapter. Raw mapping state stays source-owned. The gateway certificate is insufficient to call either route.

`SOURCE_TLS_PORT` defaults to 8444. Optional `MOCK_VERIFICATION_GATE_DIRECTORY` is a private local test control directory: file `unavailable` produces 503; file `enabled` makes verification create marker `entered` and await `release` or request cancellation. These filesystem controls exist only in this explicitly enabled mock; no remote failure-control API is exposed.

The integration runner creates the CA, private configuration and databases, runs the test cases and cleans them up. Fixture balances are INR rupees and have no relation to real people. Durable production native integrations, discovery and protected disclosure are separate deliverables.
