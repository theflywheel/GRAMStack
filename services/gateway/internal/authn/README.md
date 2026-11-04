# First-party authentication and federation

Hydra v26.2.0 remains the OAuth/OIDC engine. This package implements the first-party request-scoped login client and Hydra login/consent handlers, with the independent `adapters/auth/oidc` upstream provider module. Provider-specific token authentication, cryptography settings and verified-claim normalization stay in that adapter; request/session authority stays here. It creates no citizen accounts or reusable cross-registry subject index.

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

The gateway's optional `authentication` configuration supplies `client_id`, `hydra_admin_url`, a versioned `provider` profile and `demo_upstream`. The public profile contains issuer, client ID, token authentication method, assertion audience/key ID and allowed ID-token algorithms. See `adapters/auth/oidc/README.md` for the format. Supply `GRAMSTACK_PORTAL_CLIENT_SECRET` and `GRAMSTACK_AUTH_KEY` separately. For a basic-secret upstream, set `GRAMSTACK_UPSTREAM_CLIENT_SECRET`; for eSignet's private-key authentication, set `GRAMSTACK_UPSTREAM_PRIVATE_KEY_FILE` to a protected PKCS8 PEM file and leave the upstream secret unset. Credentials are never embedded in the public profile. Startup bounds key reads and rejects mixed credentials.

Register the exact gateway `/auth/callback` with Hydra and `/auth/provider/callback` with the upstream provider. Configure Hydra login/consent URLs as gateway `/auth/login` and `/auth/oauth-consent`. The pinned eSignet profile uses PS256 assertions, issuer audience and PS256 ID tokens. An explicit demo-only profile option handles its mock backend's malformed optional JWKS certificate metadata; JOSE signature checks remain mandatory. No process-wide x509 relaxation is used.

The first-party session cannot outlive the request, the current upstream authentication, the verified Hydra token or ten minutes. An expired principal binding is retained only to enforce same-person reauthentication for that request; it cannot authorize a registry operation.

A fictional upstream declares `gramstack_demo` in discovery and is rejected unless `demo_upstream` is explicitly enabled. The gateway allows that flag only in `GRAMSTACK_ENV=development` or `demo`. HTTP is separately limited to explicitly enabled literal loopback in development. The demo IdP is under `mocks/identity`, not in core `cmd/`.

The upstream adapter supports explicitly pinned RS256/ES256/PS256 ID tokens; Hydra retains its separate RS256/ES256 verification profile. The reference supports OIDC code flow, one configured upstream provider, no remembered identity login/consent and no refresh tokens. It does not implement SAML, native government identity onboarding, registry verification, data grants or the final consent portal. Process readiness remains unavailable until the full disclosure path is configured.

## Tests

`./scripts/check-go.sh` runs race/vet checks. `PG_BIN=... ./scripts/test-postgres.sh` exercises encrypted state, wrong binding/key, expiry, authenticated-expiry tampering, atomic consumption, restart and session/CSRF rules.

`PG_BIN=... HYDRA_BIN=... GRAMSTACK_TEST_AUTHN=1 ./scripts/test-hydra.sh` starts isolated PostgreSQL, official Hydra, the separate fictional IdP and the configured gateway. It runs the entire redirect/code exchange chain and checks wrong-browser callback, replay, request cancellation/expiry and different-person reauthentication. Authentication stops at BINDING_PENDING with no disclosure consent. CI runs consumer-only and federation variants. Negative token tests cover issuer, audience, nonce, expiry, future issuance, subject, signature and forbidden token-endpoint redirects.

`GRAMSTACK_TEST_ESIGNET=1 PG_BIN=... HYDRA_BIN=... ./scripts/test-hydra.sh` runs the real eSignet alternative against the loopback stack in `infra/esignet`. The development-only `tools/esignet-demo` onboards an ephemeral client/key and fictional Sunita/Rani identities. The test reaches the official UI URL, executes its mock OTP flow API, completes both code exchanges, and checks state/browser binding, callback replay, capped session lifetime, same-person renewal, different-person denial and late callbacks after cancellation/expiry. It checks no disclosure consent was created. This is full HTTP federation integration; rendered UI interaction and recording remain separate browser acceptance work.

Before module releases, root `go.work` resolves the auth adapter locally. Standalone CI/integration runners use `scripts/gateway-module.sh` to create temporary module files; no local replace is committed in `go.mod`, and public checksum verification is not disabled. D13 tracks release versions/private-module distribution. The current repository is private and cannot be fetched through the public Go checksum service without appropriate distribution configuration.

## Consent history ownership

For consent-enabled interactions, final authentication commits an opaque
history owner binding together with the encrypted browser session. The
binding is derived from the verified upstream issuer and subject, scoped to
the configured provider client and registry, using a domain-separated key.
The request keeps its original independent principal reference. The
history table stores neither the upstream subject nor an identity-provider
token.

Keep the authentication key stable while these records must be recoverable.
Changing the key or provider client changes the derived owner scope. Existing
records without a binding require verified reauthentication to establish a
link. The binding is internal groundwork for a separately authenticated
history reader.

Open `/consent-history/{registry}` to start a separate sign-in. The history
flow reuses the configured provider callback and the adapter's forced login,
with its own browser-bound one-use state, nonce, PKCE and cookie. The callback
requires authentication at or after the start of this flow. The history session
is registry-scoped and lasts at most five minutes, bounded by token expiry.
It authorizes only the consent history list and receipt download. The list
paginates in batches of 25 and only includes rows linked to the verified owner
and original request principal.
