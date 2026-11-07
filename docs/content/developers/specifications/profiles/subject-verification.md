---
title: Registry subject verification
description: Versioned GRAMStack implementation contracts and conformance requirements.
---

# Registry subject verification profile 0.1.0

This independently versioned profile defines the trusted gateway-to-adapter verification exchange. It adds a concrete verifier transport alongside immutable core contract 0.2.0. It does not change the published 0.2.0 PrincipalContext schema. Its profile-specific principal optionally carries upstream authentication methods and assurance provenance; absent values create no assurance claim.

## Contract and version binding

An approved registry descriptor MUST pin `verification_contract_version: "0.1.0"`, registry ID, HTTPS origin, server certificate identity, verification method and explicit demo permission where applicable. No user-supplied endpoint or method is used.

POST `/internal/v1/subject-verifications` requires service mTLS, the configured gateway certificate identity, `Content-Type: application/json` and header `Gramstack-Subject-Verification-Version: 0.1.0`. The response repeats the version header. Unsupported/missing versions fail closed; there is no silent fallback. The SDK additionally rejects unknown or duplicate JSON fields, null input, oversized bodies and malformed identifiers.

Request and response shapes are in [schemas.json](https://docs.gramstack.try-dough.com/static/specs/subject-verification/0.1.0/schemas.json); [examples.json](https://docs.gramstack.try-dough.com/static/specs/subject-verification/0.1.0/examples.json) supplies positive/negative fictional fixtures. The request includes the accepted immutable snapshot, its RFC 8785 digest and server-derived verified principal. The adapter recomputes the digest, checks registry/resource/fields and bounds, and invokes only a registry-approved subject verifier. A locator and successful login alone are insufficient.

Request deadline and principal expiry MUST remain in the future. The reference profile accepts signed authentication within five minutes with at most 30 seconds of forward clock skew. Requested single-use validity is 1–300 seconds; accepted request duration is at most ten minutes. The returned context expires no later than authentication, request expiry or five minutes after verification. Method/assurance values preserve their issuer provenance and MUST NOT be interpreted as invented assurance equivalence.

## Custody, idempotency and outcomes

The adapter stores its opaque context, request digest, principal reference and encrypted source mapping in its own PostgreSQL database, with database-time TTL enforcement. The encryption authenticates the request ID, context ID, digest, principal and expiry metadata. Concurrent retries of a live request/digest/principal tuple return the same immutable context. A conflicting source mapping is rejected. Expired contexts confer no authority; fresh verification may allocate a new context after the expired tuple is removed.

The gateway validates the adapter result and commits encrypted context custody, the request FSM transition from BINDING_PENDING to CONSENT_PENDING and the outbox event in one transaction. It holds no SQL lock during native verification. Cancellation/deadline/version checks run again under the authoritative transaction. A late callback cannot revive cancelled or expired authority. A failed context write rolls back both state and event; an adapter context left behind by a failed gateway commit is unusable by itself and expires by TTL.

200 returns only a verified context to the authenticated gateway. 400 rejects malformed/unsupported contracts, 403 rejects caller/ownership/binding proof, and 503 reports a mandatory verifier/storage outage. Public browser responses expose only an opaque context reference and next action, never source mappings, private identity or business rows. The browser POST `/interactions/{id}/verify` requires request-bound session, exact Origin and CSRF; it accepts no identity or source-record override. Verification is synchronous for that interaction; the consumer's previously accepted workflow remains asynchronous and pollable.

## Reference demo boundaries and conformance

The LokOS reference adapter uses the Go SDK. Its separate native mock owns explicit fictional identity-to-record bindings. The eSignet fixture tool derives pairwise subjects through actual OIDC verification for the fictional Sunita/Rani accounts; those mappings are local operator fixtures, not a production registry ownership mechanism. The gateway contains no LokOS-specific identity mapping. Demo modes are rejected in production configuration, and a native response marked demo is rejected without explicit permission.

Conformance requires: wrong member/person/issuer denial; expired/stale authentication and request denial; untrusted service and missing certificate rejection; no direct gateway-to-native data bypass; exact contract version and strict JSON; stable concurrent retries; encrypted custody and metadata tamper rejection; cancellation during native verification; rollback without authority/event leakage; source outage failure; and gateway/adapter/source restart recovery. Verification MUST NOT create disclosure consent or return business data. Protected data retrieval and disclosure evidence follow the separate core adapter contract.

[Download schemas](https://docs.gramstack.try-dough.com/static/specs/subject-verification/0.1.0/schemas.json) · [Download fixtures](https://docs.gramstack.try-dough.com/static/specs/subject-verification/0.1.0/examples.json)
