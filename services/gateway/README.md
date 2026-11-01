# GRAMStack gateway

Independent Go module for the core application, including the explicit Consent Manager subsystem. Request and consent transition decisions use `github.com/looplab/fsm`; persisted state must still be changed with SQL expected-version checks. The request transition graph is checked against `specs/0.1.0/lifecycle.json` in CI.

Run `go run ./cmd/gramstack`. PORT defaults to 8080. `/healthz` reports process liveness; `/readyz` returns 503 until application dependencies are implemented and configured. Protected application routes are not exposed by the bootstrap executable.

## Durable state

`migrations/` contains embedded, checksummed migrations applied under a database advisory lock. Run `DATABASE_URL=... go run ./cmd/gramstack migrate` as an explicit deployment operation. The access repository commits FSM transitions with version checks and metadata outbox events. The enforcement repository serializes withdrawal and single-use redemption by locking the same consent row, retaining consumed reservations across later withdrawal.

These repositories are internal primitives. Authenticating actors, validating signed grants and recording valid human approval/activation must precede their protected use; no HTTP bypass is exposed. The integration tests create ACTIVE consent only as isolated SQL fixtures while the Consent Manager's signing/activation flow is implemented.

Run `PG_BIN=... scripts/test-postgres.sh` from repository root for real database concurrency tests. The runner owns and removes its own private-socket PostgreSQL cluster and never connects to production.
