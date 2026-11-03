# End-to-end demo delivery checklist

Owner: this repository. Cross off a task only after its acceptance gate passes and link the evidence below. Keep tested increments on main. The original T01–T20 ledger remains in `IMPLEMENTATION.md`; D tasks below group the remaining work into demo delivery phases.

## Target

A recorded, reproducible SakhiCredit journey: authenticated consumer request → Sunita authentication and registry subject verification → exact notice and explicit approval → scoped one-use grant → SDK-backed LokOS retrieval → durable evidence and DeDi commitment → approved data in the product. Show rejection, cancellation/withdrawal and replay prevention, plus honest integration boundaries.

## Phase A — authority and request foundation

- [x] D01 Cancellation and withdrawal enforcement. Owner-checked cancellation before consent and after approval blocks future activation/admission; after admission report cutoff passed. Test activation/redemption races, retries, rollback and late callbacks against PostgreSQL. No partial refusal/revision semantics.
- [x] D02 Consumer authentication and request APIs. Hydra authentication, immutable snapshot/digest, allowlists, terms binding, idempotent 202 creation, owner-only polling and cancellation. Test invalid/expired tokens, cross-consumer access, changed idempotency payloads and restarts.

Gate A: D01–D02 pass integration/contract checks; an authenticated consumer can create, read and cancel a durable request. No business data routes enabled yet.

## Phase B — person, subject and consent

- [x] D03 Authentication and first-party sessions. Federated code flow, state/nonce/PKCE, pinned issuer/audience verification, secure cookies, CSRF, expiry and reauthentication. Test callback replay, session substitution and untrusted redirects.
- [ ] D03a Real eSignet provider integration. Use the official eSignet service and UI with its mock identity backend; versioned auth-provider contract/profile, private_key_jwt client authentication and PS256 ID-token verification. Exercise successful login, invalid assertion, state/nonce/PKCE and request-bound reauthentication. Generic OIDC test coverage alone does not close this gate.
- [ ] D04 Registry subject verification. Registry-approved demo verifier binds a fictional source record to request and principal. Locator/login alone is insufficient. Demonstrate incorrect-member denial and test-only verifier rejection in production configuration.
- [ ] D05 Consent portal. Presentable exact notice with recipient/retention terms, approve/reject, withdrawal and receipts. Authenticated sessions and reviewed supported language templates; explicitly disclose unsupported languages.
- [ ] D06 Durable workers. Lease/ack/retry signing, activation, expiry, notifications and reconciliation. Test competing workers, outage recovery and process restart without duplicated authority.

Gate B: D03, D03a and D04–D06 pass. A real browser journey can activate consent or reject it; cancellation/withdrawal cannot be undone by delayed work.

## Phase C — protected disclosure

- [ ] D07 Policy, grants and redemption API. Current source policy/consent/subject/terms checks; ES256 scoped short-lived grants and sender binding. Wrong audience, fields, identity and stale policy fail closed.
- [ ] D08 Protected SDK retrieval. Service/presenter authentication, independent grant checks, atomic redemption, durable attempts, exact projection and evidence-before-release. Concurrent/replayed or ambiguous attempts never perform a second fetch.
- [ ] D09 Separate mock LokOS and adapter discovery. Fictional source service under `mocks/`, predictable failure controls, SDK-backed adapter and capability/key validation. No registry data or production credentials in fixtures.
- [ ] D10 Evidence custody and automated DeDi anchoring. Immutable signed receipts plus atomic anchoring outbox, verified actual-node publication, idempotency and outage recovery. Exercise the selected synchronous/batch release profile honestly.

Gate C: D07–D10 pass. Exactly approved fields cross the adapter boundary once and only after durable evidence. Source/evidence failures cannot bypass the gate.

## Phase D — product and deployment

- [ ] D11 SakhiCredit product. Independent consumer backend and presentable web UI: create application, progress, interaction handoff, approved result and graceful rejection/expiry/outage states. No business decision logic in platform core.
- [ ] D12 Reproducible local and Railway stack. Fresh setup, isolated credentials, migrations, dependency readiness, correct public/private routing and service health. Docs stays an independent service; eSignet is required for the demo.
- [ ] D13 Published SDK and adapter conformance. Real module-prefixed version, independently buildable LokOS dependency, shared positive/negative contract suite and external consumer smoke check.
- [ ] D14 Design additions exercised by the demo. Checklist below must pass or have an explicit, approved scope decision before this task can close. Unsupported features are visible in the capability report, never silently accepted.
  - [ ] Trusted versioned purpose/processing profile bindings, supported time bounds and recipient/processor responsibility metadata.
  - [ ] Channel/language presentation evidence linked to exact notice and explicit confirmation; assistance confers no authority.
  - [ ] Protected consent/disclosure history and machine-readable receipt export.
  - [ ] Authenticated downstream handling receipts with reported/verified distinction and retention exceptions.
  - [ ] Rights/grievance case creation, responsible-organisation routing and safe asynchronous status. No claim of registry mutation or guaranteed recipient deletion.

Gate D: D11–D14 pass; a fresh environment runs the browser journey using the same documented commands. Do not mark a product ready based only on backend unit tests.

## Phase E — acceptance and recording

- [ ] D15 End-to-end acceptance. Automated happy path, rejection, expiry, cancellation/withdrawal races, replay, policy denial, source/evidence outage and restart recovery; check payload leakage and bounded concurrency/load. Save redacted results and operator instructions.
- [ ] D16 Capability report and recording script. A committed working / mocked / unsupported matrix references actual test evidence and deployments. Clearly distinguish real Hydra/DeDi protocol integration from fictional LokOS records and demo identity/verifier. Explicitly list native registry onboarding, mock identity backend behind real eSignet, unimplemented authority/channel profiles, legal registration/certification and deletion-proof limits.
- [ ] D17 Record and verify the demo. Record the actual working product and platform evidence, show at least one denied/replayed path and the capability matrix, exclude credentials/personal data, play back the result and deliver the video plus reproducible run instructions. Never substitute a mockup or scripted UI animation for the live recording.

Gate E: D15–D17 pass. Final deliverables are a working product, reusable platform, reproducible environment, acceptance evidence, capability report and playable recording.

## Continuous CI acceptance

CI speed is part of each phase gate. Measure critical-path and total job time, remove duplicate setup/build work, maintain complete cache keys, and keep local feedback fast without weakening race, security, live-integration or usability checks. See [CI.md](CI.md) for coverage, optimisations and measured evidence. Current measured warm Go CI: 46 → 33 seconds elapsed and 161 → 105 summed job seconds, with all checks passing; exact dependency caches retained.

## Evidence and current work

- Baseline: `a1b6c32` — Go/SQL/DeDi CI passed; `d8d569b` — architecture v0.14 docs build/deployment passed. These are foundation checks, not end-to-end acceptance.
- D01 passed: `./scripts/check-go.sh`; `PG_BIN=/opt/homebrew/opt/postgresql@15/bin ./scripts/test-postgres.sh`; docs build/contract/content verification. Includes deterministic signing-stop tests, cancellation/redemption races, non-owner denial, idempotency, rollback and late-callback rejection. HTTP exposure follows in D02.
- D02 passed: independent Go race/vet/process checks; real PostgreSQL HTTP/concurrency/restart/rollback/deadline tests; `PG_BIN=... HYDRA_BIN=... ./scripts/test-hydra.sh` against official Hydra v26.2.0. Actual client credentials → introspection → configured running gateway create/poll/cancel passed; actual token revocation denied access. CI includes the same isolated real-Hydra test.
- Gate A passed locally and in [Go CI](https://github.com/theflywheel/GRAMStack/actions/runs/41608478751): D01/D02. No data/grant/browser routes are exposed. Snapshot cleanup, ingress rate limits and full service readiness remain explicit D06/D12 responsibilities.
- D03 passed locally: race/vet/process checks; PostgreSQL encrypted TTL/one-use/restart/session tests; official Hydra v26.2.0 → separate fictional IdP → configured gateway federation. Negative issuer/audience/nonce/signature, callback replay, wrong browser, request expiry/cancellation and different-person takeover tests pass. Authentication stops at BINDING_PENDING and creates no disclosure consent. [CI passed on 7c6b17a](https://github.com/theflywheel/GRAMStack/actions/runs/41612002970), including consumer-only and federation variants. A prior runner-only SIGPIPE failure was fixed by consuming complete Hydra version output.
- Active: Phase B, D04 registry-approved subject verification, followed by D05 portal and D06 workers. Gate B remains open.
- No demo video or end-to-end product exists at checklist creation. No D task is marked complete from documentation alone.

- eSignet infrastructure checkpoint (2026-11-03): `open -a Docker` started Docker Desktop; repository `infra/esignet/compose.yaml` validated and ran official digest-pinned services. `python3 infra/esignet/check.py` passed before and after container recreation with persistent database/key volumes. UI, health, pinned issuer/endpoints and advertised private_key_jwt/PS256 verified. D03a and D12 remain open: gateway login, fresh fictional identity seeding and hosted routing are separate acceptance gates.

- D03a adapter checkpoint (2026-11-03): independent `adapters/auth/oidc` module with versioned public profile and separate secret/key inputs. Race/vet checks and `GRAMSTACK_TEST_ESIGNET=1 GOWORK=off go -C adapters/auth/oidc test -race -count=1 ./...` passed against the official local stack: mock OTP → code + PKCE → private_key_jwt (issuer audience) → verified PS256 principal → code replay denied. Explicit demo-only public-JWK metadata transformation handles the mock backend’s invalid optional certificate; signature rejection tests still pass. Gateway integration/browser-session acceptance remains open.
