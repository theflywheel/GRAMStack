---
title: Authorization and grant contract
description: Versioned GRAMStack implementation contracts and conformance requirements.
---

Specification version **0.3.0** · [Specifications index](/developers/specifications) · [Download source](https://docs.gramstack.try-dough.com/static/specs/0.3.0/authorization.md)

# Authorization and grant contract

## Grant issuance

AuthZ evaluates the immutable request, trusted subject context, ACTIVE consent and signed registry policy. Policy MUST permit the authenticated consumer, purpose, resource and exact field subset, require verified subject and human consent, limit grant lifetime and allow one disclosure. Consent and context MUST outlive the issued grant. AuthZ signs a compact JWS with ES256, typ=gramstack-grant+jwt and a configured kid; payload is RetrievalGrant. A policy document is signed independently with typ=gramstack-policy+jws and published through operator-controlled configuration.

The grant audience is exactly the registered adapter audience, `sub` is an opaque request-scoped subject, and client_id is the consumer. `cnf.x5t#S256` binds the grant to the consumer's registered mTLS certificate thumbprint. Issuer, audience, signature, times, jti, request digest, subject context, policy version and consent scope are all mandatory checks. `exp > iat` and lifetime is at most 300 seconds and the narrower policy/consent/context deadline. No trust is inferred from claims merely being present.

Issuance is idempotent per access request. It MUST NOT create multiple usable grants that bypass one-use enforcement. One reservation is keyed by consent + request + disclosure slot, shared across all issued grant identifiers.

## Redemption transaction

The adapter validates grant and scope locally, then POSTs RedemptionRequest to AuthZ using its own service mTLS identity. AuthZ verifies that service identity owns the grant audience and that the trusted presenter binding matches the grant. In one transaction it checks live consent status, expiry, policy and subject context validity, then compare-and-swaps the disclosure slot from AVAILABLE to RESERVED and records jti, attempt_id, adapter_id and redemption_id.

An exact retry of the same attempt returns its reservation metadata with `first_redemption=false`. A different attempt for the same slot returns 409. Replayed grants, stale status versions, mismatched digests and wrong audiences never fetch source data. The adapter MUST persist its own retrieval attempt and enforce at-most-one source fetch for that attempt; a successful redemption response is not permission to repeat a source read after a crash.

Withdrawal and this transaction MUST be serialized using the same authority. A webhook, distributed cache or read-then-write check is insufficient.

## Ambiguous outcomes

After reservation, source failure, process crash, evidence failure or transport ambiguity does not unconsume the grant. Record FAILED or DELIVERY_UNKNOWN as appropriate. Operator reconciliation can describe what happened but MUST NOT silently resend plaintext. Requests for another disclosure start a new consent flow.

[Repository source](https://github.com/theflywheel/GRAMStack/tree/main/specs/0.3.0/authorization.md).
