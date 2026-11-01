# GRAMStack gateway

Independent Go module for the core application, including the explicit Consent Manager subsystem. Request and consent transition decisions use `github.com/looplab/fsm`; persisted state must still be changed with SQL expected-version checks. The request transition graph is checked against `specs/0.1.0/lifecycle.json` in CI.

Run `go run ./cmd/gramstack`. PORT defaults to 8080. `/healthz` reports process liveness; `/readyz` returns 503 until application dependencies are implemented and configured. Protected application routes are not exposed by the bootstrap executable.
