# Consent contract

## Notice and decision

The consent module accepts an authenticated, idempotent request from the gateway with the immutable request snapshot, digest and trusted VerifiedSubjectContext reference. It MUST resolve the reference, check its registry/request/expiry, recompute the request digest and store the exact notice before presenting it. Notice content states consumer, provider, resource, exact fields, purpose, single-use validity, recipient retention terms and withdrawal mechanism in the chosen language. `notice_digest` commits to the UTF-8 bytes of the stored notice; store the exact bytes, version, language and resolved terms alongside it.

GET consent returns that Notice object for the first-party UI. POST decision requires the bound browser session and X-CSRF-Token, plus decision, request_digest, notice_digest, notice_version and language. Approval is rejected if any displayed notice binding changed. A duplicate decision cannot create a second consent. Reject yields REJECTED with no retrieval authority.

## Signed artefact

The consent authority issues a compact JWS with protected `alg=ES256`, `typ=gramstack-consent+jwt` and a configured `kid`. The payload matches ConsentArtefact. A valid signature records what the authority witnessed; it is not a citizen's digital signature and not a retrieval token. Pin issuer/key use; reject algorithm substitution, unknown key IDs and untrusted key URLs. Public keys are served through the configured authority JWKS endpoint. Verification MUST check validity, request/subject commitments, consumer/provider/resource/fields/purpose/terms, and current consent status.

Approval and signed artefact durability precede ACTIVE. A signing failure remains APPROVAL_RECORDED, cannot authorize retrieval and is recovered idempotently. Current status is stored separately from the signed immutable artefact. The artefact `status_endpoint` is issuer-configured and must match a trusted origin, never an arbitrary URL to fetch.

## Withdrawal and ordering

POST withdrawal requires session ownership, CSRF validation and reauthentication according to deployment policy. Consent enforcement and grant redemption share one transactional authority. Withdrawal that commits before redemption prevents it. Redemption that commits first consumes the one-use reservation; withdrawal stops later uses but cannot recall already released data. Both race outcomes return an explicit versioned status and are audited.

Consent status and one-use accounting are separate: status is PENDING, APPROVAL_RECORDED, ACTIVE, REJECTED, WITHDRAWN or EXPIRED; use is AVAILABLE or RESERVED with a terminal outcome recorded separately. EXHAUSTED is the derived state after the allowed disclosure is reserved. A later failure cannot restore AVAILABLE automatically. Status versions are monotonically increasing and must be compared atomically, not from a stale cache.

## Events

Store each decision/status mutation and outbox event in the same SQL transaction. Events contain a stable event_id, type, consent_id, access_request_id, request_digest, status_version and occurred_at, with no business data. Publish over authenticated transport; the receiver authenticates the producer and verifies the signed event payload before processing. Compact JWS uses ES256, trusted kid and typ=gramstack-consent-event+jwt. Redelivery is at least once. Deduplicate event_id and reject stale versions. Events wake workflows; they are not the authoritative live revocation check. Reconcile with status after missed events.

Withdrawal during APPROVAL_RECORDED transitions to WITHDRAWN under the shared consent lock. Activation must recheck state/version after signing and cannot resurrect the withdrawn consent. Consumer cancellation can also invalidate PENDING consent; it records a distinct internal cancellation event rather than impersonating a human withdrawal. The signed artefact, if already created, remains immutable evidence and conveys no authority after cancellation.

## Recovery and workflow reconciliation

Consent withdrawal after APPROVAL_RECORDED can precede request activation. Request lifecycle 0.3.0 therefore permits CONSENT_PENDING → WITHDRAWN; this does not create consent, a grant or a disclosure. Delivery workers re-read current authority under request-then-consent locks rather than applying an old event status. A delayed ACTIVE notification cannot revive withdrawn, cancelled, expired or admitted authority. Signing and notification acknowledgements are independent and durable per subscriber. Leases fence acknowledgements; state/version checks still fence authoritative effects.

Retrospective rejection/withdrawal projection may finish after the interaction deadline; it records an already committed stop and creates no new approval. Authentication, subject verification and activation still require live bounds. A consumed admission is never projected back into an earlier expiry or withdrawal workflow state.
