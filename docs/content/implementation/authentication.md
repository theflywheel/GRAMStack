---
title: Authentication · Hydra and eSignet
---

The gateway integrates Hydra with an OIDC provider adapter. The deployed demo uses eSignet with fictional identity records. There are three distinct entry paths.

| Path | Result | Does it authorize retrieval? |
|---|---|---|
| Consumer client credentials | Backend access token for the enrolled application | Only request API access; consent and grants remain necessary |
| Standalone application sign-in | Verified identity scoped to the enrolled application | No |
| Request-bound person sign-in | Principal context correlated to a sharing request | No; registry ownership verification and consent follow |

Hydra's public service exposes protocol endpoints. The admin service supports privileged login, consent and client management and stays private. The gateway's existing browser handlers perform federation; there is no separate GRAMStack login service.

Standalone clients are enrolled through `authentication.application_clients` with exact callbacks. A dedicated, stable `GRAMSTACK_APPLICATION_SUBJECT_KEY` derives pairwise application identities. SDKs use PKCE, state, nonce and verified ID tokens; SakhiCredit stores encrypted one-use login state in PostgreSQL and rotates its browser session after sign-in.

The application-login client is separate from the consumer client. Changing identity providers belongs to the authentication adapter boundary and provider configuration. A real government integration still needs approved credentials, policy and operational onboarding.

[Authentication architecture](/architecture/03-authentication) · [Application authentication profile](/developers/specifications/profiles/application-auth) · [Gateway implementation](https://github.com/theflywheel/GRAMStack/tree/main/services/gateway) · [eSignet setup](https://github.com/theflywheel/GRAMStack/tree/main/infra/esignet)
