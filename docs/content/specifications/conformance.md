---
title: Conformance and evolution
description: Versioned GRAMStack implementation contracts and conformance requirements.
---

Specification version **0.2.0** · [Specifications index](/specifications/) · [Download source](https://docs.gramstack.try-dough.com/static/specs/0.2.0/conformance.md)

# Conformance and evolution

## Required behavioral scenarios

| Scenario | Required outcome |
|---|---|
| Valid SakhiCredit request and verified Sunita | Exact notice → approval → scoped grant → one LokOS read → durable evidence → exact selected fields |
| Login succeeds, source ownership unverified | No consent activation or retrieval |
| Consumer, resource, field, purpose or notice changes | Digest/notice check fails; new immutable request required |
| Wrong issuer, key, audience, presenter or expired grant | Reject before source access |
| Two concurrent attempts / grants for one disclosure | Exactly one reservation and at most one source fetch |
| Withdrawal commits before redemption | Retrieval denied |
| Redemption commits before withdrawal | Reservation stays consumed; later uses denied |
| Duplicate event or idempotency retry | Same metadata; no duplicate decision, reservation or source read |
| Same idempotency/event key with changed bytes | Conflict |
| Crash after reservation / timeout after possible release | No automatic data replay; FAILED or DELIVERY_UNKNOWN with available evidence |
| Evidence store or mandatory anchor unavailable | No release |
| Source returns extra fields | Project only the approved fields |
| Unregistered adapter URL or manifest/key mismatch | No connection/trust promotion |
| Session, CSRF, ownership or callback verification fails | No human decision recorded |
| Mock subject assertion reaches production | Rejected |

JSON Schema fixtures check message shapes, not these transactional or cryptographic guarantees. Implementations MUST exercise these scenarios against real persistence and concurrency before declaring conformance. Signing tests require actual key verification and negative algorithm/key tests. Mock demos must label source data and verification as simulated.

## Compatibility

Version 0.2.0 is the contract baseline. Breaking schema/behavior changes receive a new release directory and explicit adapter capability negotiation. Never edit a deployed signed artefact to upgrade it. Preserve old verification profiles through their evidence retention horizon. Add new resources through approved manifests and schemas rather than treating arbitrary JSON as a resource contract.

## Automated checks

`npm run verify:specs` in `docs/` validates OpenAPI, resolves its message schema references, compiles the JSON Schema, checks positive/negative examples, verifies lifecycle reachability and asserts key safety constraints in fixtures. The documentation build runs these checks and publishes exact source copies. Runtime conformance remains an implementation responsibility.

## Cancellation acceptance

Verify owner-only cancellation with privacy-safe non-owner lookup; before-consent cancellation; cancellation during signing; activation and redemption races; repeated cancellation status/version; outbox rollback; immutable signed artefact preservation; no resurrection from delayed callbacks; and 409 after a consumed reservation even when the workflow projection is stale. Consumer cancellation events must not impersonate subject rejection/withdrawal.

[Repository source](https://github.com/theflywheel/GRAMStack/tree/main/specs/0.2.0/conformance.md).
