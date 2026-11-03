# GRAMStack gateway

Independent Go module for the core application, including the explicit Consent Manager subsystem. Request and consent transition decisions use `github.com/looplab/fsm`; persisted state must still be changed with SQL expected-version checks. The request transition graph is checked against `specs/0.3.0/lifecycle.json` in CI.

Run `go run ./cmd/gramstack`. PORT defaults to 8080. `/healthz` reports process liveness; `/readyz` returns 503 until application dependencies are implemented and configured. Without explicit consumer configuration, the bootstrap executable exposes no protected application routes.

## Durable state

`migrations/` contains embedded, checksummed migrations applied under a database advisory lock. Run `DATABASE_URL=... go run ./cmd/gramstack migrate` as an explicit deployment operation. The access repository commits FSM transitions with version checks and metadata outbox events. The enforcement repository serializes withdrawal and single-use redemption by locking the same consent row, retaining consumed reservations across later withdrawal.

These repositories are internal primitives. Authenticating actors, validating signed grants and recording valid human approval/activation must precede their protected use; no HTTP bypass is exposed. Enforcement-only tests seed ACTIVE rows as isolated fixtures. The [Consent Manager tests](internal/consentmanager/README.md) additionally create notices, record decisions and activate through actual ES256 signing before exercising one-use enforcement.

Run `PG_BIN=... scripts/test-postgres.sh` from repository root for real database concurrency tests. The runner owns and removes its own private-socket PostgreSQL cluster and never connects to production.

## Consumer request API increment

Set `GRAMSTACK_CONFIG_FILE` to a reviewed configuration based on `config.example.json` and supply `DATABASE_URL`. Run the explicit `migrate` command first. This enables authenticated POST `/v1/access-requests`, owner-only GET `/v1/access-requests/{id}` and POST `/v1/access-requests/{id}/cancel`. Optional authorization configuration enables grant issuance and private admission; data routes remain unavailable. Person/registry/consent routes are described below. `/readyz` still returns 503 because the complete disclosure pipeline is not configured.

Hydra v26.2.0 introspection is pinned to a configured private/admin endpoint, issuer, audience and scope. The gateway requires an active, unexpired access token, checks token use/type and resolves the consumer from the issuer response rather than browser JSON. Keep the admin endpoint private and protected by deployment transport controls. No token or introspection result is logged or stored. Outages fail closed; redirects are rejected. HTTP is allowed only for explicit literal-loopback development endpoints with `GRAMSTACK_ENV=development`; production configuration rejects that switch.

Request creation canonicalises and validates input, rejects duplicate JSON keys/unknown fields, applies configured consumer scope/terms, and commits an immutable snapshot, request, idempotency binding and metadata outbox atomically. Snapshot storage contains the sensitive locator in protected SQL, never in public status/evidence; deployments require restricted roles, encrypted storage/backups and bounded cleanup (D06/D12). No source records are stored. Concurrent key retries and restart retries return the original 202 metadata; changed input conflicts. Polling records due expiry. Cancellation does not accept scope changes and shares the enforcement cutoff with redemption.

Terms/allowlists resolve from reviewed deployment configuration. Authorization adds signed current source policies in SQL; see below. Bounded snapshot cleanup is provided by the consent workers. Production ingress rate limiting and full disclosure readiness remain deployment responsibilities.

Run `PG_BIN=... HYDRA_BIN=... ./scripts/test-hydra.sh` from the repository root to exercise actual Hydra client credentials, introspection and revocation, then create/poll/cancel against a configured running gateway. Use an official checksum-verified Hydra v26.2.0 binary. The runner creates isolated databases/processes, uses synthetic credentials and removes them on exit. CI pins the official Linux archive SHA-256. Unit/SQL tests additionally cover wrong issuer/audience/token-use, malformed requests, scope substitution, ownership, concurrent/restart idempotency, deadlines and rollback.

## Optional citizen authentication increment

The optional `authentication` configuration enables `/auth/*` federation handlers and `/interactions/{id}`. See [AuthN](internal/authn/README.md) for protocol boundaries, secrets, cookies and tests. A completed login advances to BINDING_PENDING and does not create registry ownership, disclosure consent or a grant. Configured registry descriptors enable SDK-backed subject verification; optional consent configuration enables the first-party portal. `/readyz` continues to report unavailable for the full service.

## Consent portal

With authentication and registry descriptors configured, add `"consent": {"key_id":"reviewed-consent-key"}` and supply a private P-256 PEM through `GRAMSTACK_CONSENT_SIGNING_KEY_FILE`. Optional issuer/status origin default to the configured public origin. Production requires trusted HTTPS origins; literal loopback is available only through the existing explicit development switch. The signing key is separate from service TLS keys. Migration 0006 retains immutable session-bound WEB presentations and decision links.

The portal presents exact immutable English notices and recipient terms, requires an unchecked confirmation for approval, accepts explicit rejection, and supports owner-session withdrawal and read-only human/JSON receipts. Unknown language/action/fields, duplicate forms, CSRF/Origin/session/presentation substitution fail closed. Approval commits decision/provenance/status/outbox together and stops at APPROVAL_RECORDED; signing/activation workers run when `consent.workers_enabled` is true. This short-lived interaction is not the later history/recovery capability in D14. See `specs/consent-portal/0.1.0`.

Use `GRAMSTACK_TEST_PORTAL=1 PG_BIN=... HYDRA_BIN=... ./scripts/test-hydra.sh` for the running-process acceptance suite. Add `GRAMSTACK_TEST_ESIGNET=1` for official eSignet and `GRAMSTACK_PREVIEW=1` in an interactive terminal to keep the tested runtime online for browser inspection. Fixtures, keys and SQL state are isolated and removed on exit.

## Durable consent workers

Set `consent.workers_enabled: true` to run bounded embedded signing, gateway notification and maintenance loops. Every replica may run them: per-subscriber SQL leases and state/version checks coordinate authority. Shutdown cancels and joins workers before closing their pool. Migrations remain explicit; startup does not apply DDL. Default disabled workers retain recoverable APPROVAL_RECORDED work for an operator-controlled rollout.

Signing uses a 30-second DB lease and a 20-second handler deadline. Failed work retries with persisted 1–64 second exponential backoff; lease loss cannot acknowledge a successor's delivery. A committed effect may be delivered again after a crash, so activation returns the same immutable artefact and reconciliation re-reads current consent under request-then-consent locks. No delayed event can re-activate stopped authority. Notifications are currently internal SQL subscriber delivery to the gateway, not external HTTP webhooks.

Maintenance scans at most 32 due consents and 32 request projections per pass, removes at most 128 expired encrypted browser-state rows, and removes at most 32 terminal accepted snapshots after a **24-hour retry window after request expiry**. Consent provenance, notices, signed artefacts, consumed reservations and subscriber acknowledgements are retained for the separate evidence policy. SQL TTL checks enforce expiry even while workers are unavailable. Snapshot cleanup does not claim deletion from backups or recipients; deployment retention/key policy is separate.

Run `GRAMSTACK_TEST_WORKERS=1 PG_BIN=... HYDRA_BIN=... ./scripts/test-hydra.sh` for both disabled-worker presentation tests and enabled-worker activation/expiry/withdrawal/restart tests. Add `GRAMSTACK_TEST_ESIGNET=1` for the official local provider and `GRAMSTACK_PREVIEW=1` for the worker-enabled browser interaction. Core request lifecycle is 0.3.0, advertised on consumer responses; older contracts remain available unchanged. Full disclosure readiness remains 503 until protected retrieval and evidence gates are configured.

## Scoped grants and private admission

Optional `authorization` configuration enables Hydra-authenticated grant issuance and a separate TLS 1.3 adapter listener. See [configuration, trust boundaries and transaction details](internal/authorization/README.md). Migration 0008 stores immutable source policy versions, current policy heads, grants and issuance idempotency bindings. Signed source policy is distinct from the request allowlist. No policy signing private key is required at runtime.

Use `GRAMSTACK_TEST_AUTHZ=1 PG_BIN=... HYDRA_BIN=... ./scripts/test-hydra.sh` for the full authentication, subject, portal, workers, grant, actual mTLS and restart suite. This includes all earlier gates; add `GRAMSTACK_TEST_ESIGNET=1` for the official identity provider. Grant issuance and reservation do not fetch or release source data; the protected SDK retrieval/evidence gate is separate.
