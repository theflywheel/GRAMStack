# First-party authentication and federation

Hydra v26.2.0 remains the OAuth/OIDC engine. This package implements the first-party request-scoped login client and Hydra login/consent handlers, with an upstream provider adapter using coreos/go-oidc v3.21.0 and golang.org/x/oauth2. It creates no citizen accounts or reusable cross-registry subject index.

## Flow

1. Resolve the accepted immutable access request; create a browser-bound state, nonce and PKCE transaction.
2. Begin the Hydra code flow. The login handler resolves the Hydra challenge through the pinned admin endpoint and checks first-party client, redirect URI, original state and browser cookie.
3. Start a separate upstream state/nonce/PKCE transaction. Exchange the upstream code using the confidential client and verify issuer, single audience, approved signature algorithm/key, expiry, nonce, token hash when present and fresh authentication time.
4. Keep the upstream identity only in encrypted, expiring request-scoped state. Accept the Hydra challenge with an opaque transaction subject and `remember: false`.
5. Accept OAuth consent only for the configured first-party `openid` client; reject additional scopes. This never grants registry disclosure consent or offline access.
6. Consume the outer transaction once, exchange the Hydra code with PKCE, validate its ID token and matching opaque subject, and advance only to BINDING_PENDING.
7. Rotate the session, remove the flow cookie and require a separate registry ownership check and explicit disclosure decision.

Callbacks are browser-bound and one-use. A different browser cannot consume the rightful browser's transaction. Request cancellation/deadline is checked again after authentication. Reauthentication for an existing request requires a fresh proof of the same upstream issuer/subject; it reuses only that request's opaque principal reference. This does not grant ownership of any source record.

## TTL storage and keys

The minimal deployment implements shared TTL state in PostgreSQL `browser_state`, with expiry enforced against database time on every read and atomic DELETE RETURNING for one-time consumption. Payloads are AES-256-GCM encrypted with kind, digested key/binding and expiry authenticated as associated data. Keys and browser binders are digested for lookup. Physical cleanup and operational/Hydra janitor scheduling belong to D06; database TTL checks remain mandatory even if cleanup is delayed.

Set `GRAMSTACK_AUTH_KEY` to a base64-encoded random 32-byte secret through deployment secret management. It must be stable across instances/restarts. Changing this key invalidates active encrypted flows/sessions; overlapping key rotation is not implemented by this increment. Protected database/backups and retention still matter. Upstream ID tokens, access tokens, PKCE verifiers and raw claims never enter response URLs or routine logs; protocol tokens are exchanged in memory. The encrypted store holds only needed flow secrets and normalised, expiring principal/session context.

HTTPS uses Secure, HttpOnly, SameSite=Lax `__Host-` cookies. Literal-loopback development explicitly uses distinct non-`__Host-` cookie names. Session ownership is checked per access request. Mutating portal routes use the session CSRF token and exact allowed Origin; the portal handlers are supplied separately. Only one active login flow/session is maintained per browser in this profile; another tab's login may require restarting the earlier flow.

## Configuration and supported scope

The gateway's optional `authentication` configuration supplies `client_id`, `hydra_admin_url`, `upstream_issuer`, `upstream_client_id` and `demo_upstream`. Supply `GRAMSTACK_PORTAL_CLIENT_SECRET`, `GRAMSTACK_UPSTREAM_CLIENT_SECRET` and `GRAMSTACK_AUTH_KEY` separately in the environment. Register the exact gateway `/auth/callback` with Hydra and `/auth/provider/callback` with the upstream provider. Configure Hydra login/consent URLs as gateway `/auth/login` and `/auth/oauth-consent`.

A fictional upstream declares `gramstack_demo` in discovery and is rejected unless `demo_upstream` is explicitly enabled. The gateway allows that flag only in `GRAMSTACK_ENV=development` or `demo`. HTTP is separately limited to explicitly enabled literal loopback in development. The demo IdP is under `mocks/identity`, not in core `cmd/`.

This increment supports OIDC code flow with RS256/ES256 ID tokens, one configured upstream provider, no remembered identity login/consent and no refresh tokens. It does not implement SAML, native government identity onboarding, registry verification, data grants or the final consent portal. Process readiness remains unavailable until the full disclosure path is configured.

## Tests

`./scripts/check-go.sh` runs race/vet checks. `PG_BIN=... ./scripts/test-postgres.sh` exercises encrypted state, wrong binding/key, expiry, authenticated-expiry tampering, atomic consumption, restart and session/CSRF rules.

`PG_BIN=... HYDRA_BIN=... GRAMSTACK_TEST_AUTHN=1 ./scripts/test-hydra.sh` starts isolated PostgreSQL, official Hydra, the separate fictional IdP and the configured gateway. It runs the entire redirect/code exchange chain and checks wrong-browser callback, replay, request cancellation/expiry and different-person reauthentication. Authentication stops at BINDING_PENDING with no disclosure consent. CI runs consumer-only and federation variants. Negative token tests cover issuer, audience, nonce, expiry, future issuance, subject, signature and forbidden token-endpoint redirects.
