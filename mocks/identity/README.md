# Fictional OIDC identity provider

Independent Go module for repeatable demo/protocol tests. This is not a production identity authority and performs no Aadhaar, eSignet, OTP or government identity verification. The UI labels its users fictional.

Run `go run ./cmd/demo-idp` with `GRAMSTACK_ENV=development` (or explicit `demo`), `PORT`, `OIDC_ISSUER`, `OIDC_CLIENT_ID`, `OIDC_CLIENT_SECRET` and the exact `OIDC_REDIRECT_URI`. Any other environment fails startup. Discovery marks the provider `gramstack_demo`; the gateway must explicitly permit that marker outside production.

It implements discovery, JWKS, explicit fictional-user selection, an RS256-signed ID token, nonce, S256 PKCE, confidential-client token exchange, short expiries and single-use codes. Only Sunita and Rani are available. Codes/forms are in-memory test state and disappear on restart; the queue is bounded. This mock is not an SDK or a core service dependency for other identity providers.

Run `GOWORK=off go test -race ./...`. Tests cover required demo mode, form binding, incorrect PKCE, successful exchange and replay rejection. The gateway's real-Hydra federation runner additionally verifies the full downstream OIDC flow.


Browser form navigation allows the registered callback origin and optional exact `OIDC_FORM_RETURN_ORIGINS` (comma-separated broker return origins). Chrome checks the full OAuth form redirect chain. These CSP sources are frozen at configuration time, reject wildcards/paths/credentials, and never register additional OAuth redirect URIs; authorization still requires the exact configured callback. The isolated runner supplies its Hydra public origin. This component remains an explicitly enabled fictional provider, not the eSignet service or production authentication.
