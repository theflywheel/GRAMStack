# Implementation tasks

This is the internal delivery ledger. Versioned protocol contracts live under `specs/`; public architecture lives under `docs/`. Each task closes only after its acceptance checks pass. Push tested increments to main.

## Milestone 1 — executable foundations

| ID | Task | Depends on | Acceptance |
|---|---|---|---|
| T01 | Independent gateway, SDK and LokOS modules; local workspace and Go CI | — | No root go.mod; LokOS imports SDK; each module builds/tests without relying on implicit workspace resolution |
| T02 | Request and consent FSMs using a maintained Go FSM library | T01 | All allowed/forbidden transitions tested, terminal states immutable, request graph matches versioned spec |
| T03 | Process startup, shutdown, health and readiness | T01 | Executables run; unsupported/protected routes cannot return business data; clean shutdown |

## Milestone 2 — adapter SDK and reference integration

| ID | Task | Depends on | Acceptance |
|---|---|---|---|
| T04 | SDK resource/provider contracts and exact field projection | T01 | Unsupported/duplicate fields rejected before source access; source extras cannot leak; LokOS uses SDK |
| T05 | SDK signed-grant validation and mTLS service/presenter enforcement | T04 | Wrong issuer, key, algorithm, audience, certificate or times fail before source read |
| T06 | SDK retrieval pipeline and durable attempt store | T05,T11,T13 | Concurrent/repeated attempts fetch at most once; no replay after crash/ambiguity; evidence required before release |
| T07 | Mock LokOS source and registry-approved demo subject verifier | T04 | Separate mocks/ service with fictional data; test-only verification rejected in production |
| T08 | Adapter discovery and conformance suite | T06,T07 | Approved endpoint/key/capability matching; SDK-backed LokOS passes common positive/negative cases |

## Milestone 3 — gateway, identity and consent

| ID | Task | Depends on | Acceptance |
|---|---|---|---|
| T09 | PostgreSQL migrations, transactional repositories, durable jobs/outbox | T02 | Restart recovery, CAS versioning, idempotency conflicts and concurrent workers tested against real SQL |
| T10 | Consumer authentication, immutable access requests, polling and lifecycle | T09 | Hydra-authenticated ownership, 202 acceptance, exact digests and bounded deadlines |
| T11 | Consent Manager and shared enforcement transactions | T09 | Exact notices, decisions, signed artefacts, activation and atomic withdrawal/redemption race tests |
| T12 | AuthN federation and subject-verification flow | T10 | State/nonce/PKCE, provider verification, bounded contexts and no assumption that login proves record ownership |
| T13 | Policy evaluation, grants and atomic redemption API | T11,T12 | Signed scoped grant; one disclosure reservation shared across grant IDs; no stale-cache authorization |

## Milestone 4 — accountable evidence and DeDi

| ID | Task | Depends on | Acceptance |
|---|---|---|---|
| T14 | DeDi-node signed publication and protected commitment client | T01 | Actual DeDi wire protocol; signed create-only writes; trust-pinned inclusion verification; real-node integration test |
| T15 | Evidence custody and durable anchoring worker | T09,T14 | Signed receipts/outbox commit atomically; deduplication/conflicts; synchronous/batch release gates; retry recovery |

## Milestone 5 — presentable product and infrastructure

| ID | Task | Depends on | Acceptance |
|---|---|---|---|
| T16 | First-party interaction portal | T11,T12 | Authentication, exact notice, approve/reject, withdrawal and receipts with session/CSRF checks |
| T17 | SakhiCredit web and independent consumer backend | T08,T10,T13,T16 | Presentable end-to-end Sunita journey, progress and approved data; graceful denial/expiry/ambiguity |
| T18 | Reproducible local stack and Railway service graph | T15,T17 | Separate services/credentials, migrations, health checks and tested fresh deployment; eSignet optional |

## Milestone 6 — release and verification

| ID | Task | Depends on | Acceptance |
|---|---|---|---|
| T19 | Versioned SDK release and independently consumable LokOS build | T08 | Module-prefixed SDK tag, pinned adapter dependency, no committed local replace; external consumer smoke test |
| T20 | Demo acceptance, failure matrix, load/recovery tests and operator guide | T18,T19 | Happy path plus rejection, withdrawal race, replay, source/evidence outage and restart scenarios; no payload leakage |

## End-to-end delivery

The executable demo work is tracked as cross-off tasks and gated phases in [DEMO-TODO.md](DEMO-TODO.md), including the capability report and recorded demo. D tasks map the remaining T tasks into delivery order; only passed gates are checked.

## Working increments

- Completed: T01 (independent development modules/CI), T02 (library-backed request and consent FSMs), T03 (process shells and live HTTP/shutdown checks), T04 (SDK provider contract and field projection), T14 (signed DeDi client, proof verification, actual-node and gateway CLI integration).
- In progress: T09/T11 persistence slice adds embedded checksummed migrations, request FSM CAS, transactional outbox and the common withdrawal/redemption row-lock authority. Real PostgreSQL tests cover concurrent single-use admission, retries, conflicting grants, expiry and withdrawal ordering.
- Completed T11 application-core slice: immutable English notices, trusted context/terms bindings, principal-bound decisions, ES256 signed artefacts, atomic verified activation, read-only artefact retrieval and idempotent expiry. Real database tests exercise the signed activation → one-use redemption → withdrawal path.
- Remaining T09/T11 integration: durable worker scheduling/event delivery and authenticated HTTP/session/CSRF wiring. Enforcement-only fixtures still isolate SQL race behavior; Consent Manager integration tests use the actual signed workflow.
- Next: wire consumer authentication and first-party sessions/subject contexts, then scoped grant issuance/validation and the protected SDK retrieval pipeline.
- T03 readiness intentionally reports unavailable until application dependencies are implemented; the process shells do not claim an operational data-sharing service.
- DeDi inclusion is verified, while witness/consistency policy and durable custody/outbox recovery remain T15. No full-system conformance claim is made.
- `looplab/fsm` is used for FSM decisions. SQL expected-version updates will own concurrency; an in-memory FSM lock does not provide distributed consistency.
- DeDi targets the adjacent theflywheel/DeDi-node implementation and its signed publication/transparency APIs, not a guessed generic audit API.
- SDK dependency `v0.0.0` is an unreleased local-development marker, resolved by go.work. Independent CI substitutes a temporary module file until T19 publishes a real version; no local replace is committed in adapter go.mod.
