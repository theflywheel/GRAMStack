# GRAMStack gateway

Independent Go module for the core application, including the explicit Consent Manager subsystem. Request and consent transition decisions use `github.com/looplab/fsm`; persisted state must still be changed with SQL expected-version checks. The request transition graph is checked against `specs/0.2.0/lifecycle.json` in CI.

Run `go run ./cmd/gramstack`. PORT defaults to 8080. `/healthz` reports process liveness; `/readyz` returns 503 until application dependencies are implemented and configured. Without explicit consumer configuration, the bootstrap executable exposes no protected application routes.

## Durable state

`migrations/` contains embedded, checksummed migrations applied under a database advisory lock. Run `DATABASE_URL=... go run ./cmd/gramstack migrate` as an explicit deployment operation. The access repository commits FSM transitions with version checks and metadata outbox events. The enforcement repository serializes withdrawal and single-use redemption by locking the same consent row, retaining consumed reservations across later withdrawal.

These repositories are internal primitives. Authenticating actors, validating signed grants and recording valid human approval/activation must precede their protected use; no HTTP bypass is exposed. Enforcement-only tests seed ACTIVE rows as isolated fixtures. The [Consent Manager tests](internal/consentmanager/README.md) additionally create notices, record decisions and activate through actual ES256 signing before exercising one-use enforcement.

Run `PG_BIN=... scripts/test-postgres.sh` from repository root for real database concurrency tests. The runner owns and removes its own private-socket PostgreSQL cluster and never connects to production.

## Consumer request API increment

Set `GRAMSTACK_CONFIG_FILE` to a reviewed configuration based on `config.example.json` and supply `DATABASE_URL`. Run the explicit `migrate` command first. This enables authenticated POST `/v1/access-requests`, owner-only GET `/v1/access-requests/{id}` and POST `/v1/access-requests/{id}/cancel`. Grant, data, browser interaction and identity callbacks remain unavailable. `/readyz` still returns 503 because the complete disclosure pipeline is not configured.

Hydra v26.2.0 introspection is pinned to a configured private/admin endpoint, issuer, audience and scope. The gateway requires an active, unexpired access token, checks token use/type and resolves the consumer from the issuer response rather than browser JSON. Keep the admin endpoint private and protected by deployment transport controls. No token or introspection result is logged or stored. Outages fail closed; redirects are rejected. HTTP is allowed only for explicit literal-loopback development endpoints with `GRAMSTACK_ENV=development`; production configuration rejects that switch.

Request creation canonicalises and validates input, rejects duplicate JSON keys/unknown fields, applies configured consumer scope/terms, and commits an immutable snapshot, request, idempotency binding and metadata outbox atomically. Snapshot storage contains the sensitive locator in protected SQL, never in public status/evidence; deployments require restricted roles, encrypted storage/backups and bounded cleanup (D06/D12). No source records are stored. Concurrent key retries and restart retries return the original 202 metadata; changed input conflicts. Polling records due expiry. Cancellation does not accept scope changes and shares the enforcement cutoff with redemption.

This increment resolves terms/allowlists from immutable deployment configuration; dynamic source-policy administration and grant-time current-policy checks belong to D07. Production ingress rate limiting, snapshot cleanup workers and end-to-end readiness are not supplied by this increment.

Run `PG_BIN=... HYDRA_BIN=... ./scripts/test-hydra.sh` from the repository root to exercise actual Hydra client credentials, introspection and revocation, then create/poll/cancel against a configured running gateway. Use an official checksum-verified Hydra v26.2.0 binary. The runner creates isolated databases/processes, uses synthetic credentials and removes them on exit. CI pins the official Linux archive SHA-256. Unit/SQL tests additionally cover wrong issuer/audience/token-use, malformed requests, scope substitution, ownership, concurrent/restart idempotency, deadlines and rollback.

## Optional citizen authentication increment

The optional `authentication` configuration enables `/auth/*` federation handlers and `/interactions/{id}`. See [AuthN](internal/authn/README.md) for protocol boundaries, secrets, cookies and tests. A completed login advances to BINDING_PENDING and does not create registry ownership, disclosure consent or a grant. The interaction currently shows a plain authenticated checkpoint; the consent product UI and source verification are distinct subsequent tasks. `/readyz` continues to report unavailable for the full service.
