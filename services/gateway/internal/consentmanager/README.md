# Consent Manager application core

This subsystem now implements the internal notice → decision → signed activation lifecycle against PostgreSQL. `looplab/fsm` validates transitions and the same `consent_state` row remains authoritative for activation, withdrawal, expiry and redemption.

## Operations

- `Create`: recompute the RFC 8785 request digest; match the accepted gateway request; resolve and bind the trusted subject context and immutable recipient terms; persist an exact notice. Identical request retries return the stored notice; substitutions fail.
- `GetNotice`: return the stored notice only for the principal reference bound by subject verification.
- `Decide`: bind approve/reject to the request digest, exact notice digest, version, language and authenticated principal. Decision, state and metadata outbox commit together. Duplicate identical decisions return current status; conflicting decisions fail.
- `Activate`: sign the fixed approval payload outside the SQL lock, verify ES256 signature/issuer/key/type and exact payload, then atomically persist the immutable artefact, ACTIVE status and outbox event. Failed signing/verification/storage leaves approval unusable. Concurrent activations return the same stored JWS.
- `GetArtefact`: fetch immutable signed bytes without implicitly activating or extending consent.
- `Status`: expose current persisted state and derive EXHAUSTED when an ACTIVE consent has a reserved use.
- `Expire`: idempotently apply due expiry under the shared consent lock without restoring a consumed use.

The initial notice template is English (`gramstack.en.v1`). Other languages require a reviewed template/version; unknown languages fail rather than being mislabeled.

## Authority and integration

`SubjectResolver` must resolve a registry-approved, request-bound verification context. `TermsResolver` must resolve the consumer's exact immutable policy version; the UTF-8 text digest must match the request snapshot. Source hints/mapping values are hashed transiently where necessary; durable consent scope excludes the raw hint and source mapping. Resolver outages return `ErrUnavailable` without persisting a denial; a resolver can return `ErrBinding` for a known invalid context or policy.

`Decide` takes the principal reference derived by the first-party session layer, never one supplied as browser identity input. Its HTTP caller must authenticate the session and verify CSRF. Internal operations require authenticated service routing. The optional configured first-party portal mounts authenticated transport/session integration and requires presentation proof. Full-service readiness remains unavailable until the disclosure pipeline is configured.

The signer is an injected port for local keys or a future remote signer. `NewES256Signer` uses go-jose with a configured P-256 key; the verifier pins public keys and issuer/status origins. `JWKS` exposes public signing keys only. Verification rejects algorithm substitution, unknown keys, wrong protocol types, key URLs, duplicate claims, invalid timing and altered payloads. Signature verification alone does not authorize retrieval: current SQL consent/use status and registry policy remain mandatory.

Approval records `consent.approval_recorded` as an **internal** outbox work item. It is not a versioned external ConsentEvent. ACTIVE/rejected/expired metadata uses the corresponding event types; durable signing and internal gateway delivery now run in `internal/consentworker` when explicitly configured. External signed webhook delivery is not supplied by that internal subscriber.

## Verification

Run `scripts/check-go.sh` from repository root for independent builds, race tests, vet and process smoke checks. `PG_BIN=... scripts/test-postgres.sh` additionally runs real Consent Manager and enforcement integration tests in an isolated temporary PostgreSQL cluster.

Tests cover exact notice binding, principal ownership, request/context/terms substitution, decision and activation retries/concurrency, signer outages, validly signed scope substitution, expiry, SQL rollback, immutable rows, real CM activation followed by one-use redemption, and withdrawal without artefact mutation or consent resurrection. The resolver and identity fixtures are test-only; these tests do not establish a complete browser authentication or production registry integration.

## WEB presentation and read-only records

With `RequirePresentation`, `Decide` additionally checks an unexpired presentation matching the exact notice, principal and session digest. `Present` creates stable immutable WEB/English evidence; `Record` returns a repeatable-read owner-only view and never activates. The approval checkbox is recorded separately from the explicit approve/reject action. Missing proof and presentation/session substitution are denied; decision/provenance/outbox failures roll back atomically. Legacy internal fixtures without this optional integration never imply browser presentation proof. The runtime portal always enables it. See the independent consent-portal 0.1.0 contract.

The worker tests use isolated SQL schemas and verify competing leases/sweepers, lost acknowledgements, signing outages/backoff, current-state projection, late stops, expiry without polling, consumed cutoffs, bounded browser/snapshot cleanup and rollback. The runtime process suite verifies pending work and immutable signatures across actual gateway/adapter/source restarts. These checks do not stand in for the later policy/grant/data/evidence release pipeline.
