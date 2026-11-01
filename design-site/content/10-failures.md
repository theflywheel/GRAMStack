---
title: 10 · Failure and security review
description: Negative cases, race conditions, recovery and conformance requirements.
date: 2026-11-01
---
# Failure and security review

## Failure matrix

| Failure | Required behavior | Evidence/state |
|---|---|---|
| Consumer invalid or disabled | Deny before protected workflow/redeem | Minimal security event |
| Unsupported registry/resource/fields | Reject schema/capability mismatch | Safe validation result |
| Guessed member ID | Never treat hint as ownership | No existence leak to unauthorized caller |
| Wrong IdP issuer/audience/nonce | Reject authentication | Correlated security event; consume invalid transaction safely |
| Operator account used as subject proof | Do not assume member ownership | Gate remains unresolved/failed |
| Verification missing, stale or ambiguous | Fail closed before consent activation/grant | Protected failure reference |
| Consent scope or digest mismatch | Reject; no silent narrowing/expansion | Integrity event |
| Human rejects | Terminal rejection | Appropriate notice/decision evidence |
| Consent withdrawn or expired | Deny issuance/redemption per atomic cutoff | Current authoritative status and evidence |
| Duplicate/reordered callback | Deduplicate; stale event cannot resurrect state | Event ID/version |
| Concurrent grant requests | Recover stable issuance result | Unique request/issuance record |
| Multiple grants for one consent | One-use constraint enforced across all JTIs | Shared consent-use reservation |
| Concurrent redemption | Single admitted attempt | Unique redemption and reservation |
| Source timeout after admission | Preserve consumed status; classify ambiguity | Attempt record and failure evidence |
| Native response adds unexpected fields | Allowlist/filter; fail on invalid mapping | No accidental broad JSON pass-through |
| Mandatory evidence unavailable | Withhold protected release | Durable recovery metadata |
| Crash after evidence before release | Do not claim delivery; reconcile intent | No automatic grant reset |
| Crash after release begins | DELIVERY_UNKNOWN where appropriate | Preserve attempted-release evidence |
| Cleanup worker delayed | Reject late actions by business deadline regardless of key existence | Recovery backlog; no TTL-as-authorization |

## Threat boundaries

### Request and identity substitution

A malicious consumer may change fields, purpose, recipient or member hint after Sunita sees the notice. Bind each authority artefact to the immutable request digest and verified subject context. Server-side transaction correlation prevents swapping a different login or callback into the interaction.

A phone OTP authenticates channel control under a defined assurance profile. It does not establish that the phone holder owns the requested LokOS savings record. A registry IdP must establish member-account semantics explicitly; operator credentials are not universal person identities.

### Browser interaction attacks

Secure, HttpOnly cookies, appropriate SameSite settings, session rotation, CSRF tokens, safe return URLs and short transaction expiry are needed. No state-changing approval or withdrawal GET route. Avoid sensitive information in URLs, referrers, analytics and logs. Interaction locators are unguessable but are not the sole authorization mechanism.

### Service and adapter bypass

Internal APIs require authenticated service identities and least-privilege network paths. Adapter source credentials are dedicated, restricted and unavailable to consumers. The adapter independently validates scopes and authoritative redemption; the gateway cannot simply assert “allowed” in a header.

Trust manifests and endpoints must be operator-approved. Do not follow attacker-controlled JWKS, callback or evidence URLs. Policy bundle signature/version validation and rollback rules are part of trusted configuration management.

### Consent and evidence abuse

A signed consent can be stale. A webhook can be delayed. A receipt can be fabricated by an untrusted signer. Validate issuer authority and current status, and ensure every protected release traverses the gate. Public commitments must resist guessing and correlation; opaque IDs are not automatically anonymous.

The consent authority signature is evidence of its process, not a cryptographic signature by Sunita. Evidence retains sufficient notice and authenticated-approval provenance for accountable review.

## Recovery invariants

1. Terminal workflow state cannot be resurrected by a stale callback.
2. SQL-enforced withdrawal/redemption wins over a lagging gateway projection.
3. A grant cannot become unused after an ambiguous network timeout.
4. A one-time consent cannot admit another attempt merely because a new grant JTI exists.
5. Evidence retry with the same event ID and content is idempotent; different content conflicts.
6. The source response is never persisted centrally to repair consumer retry semantics.
7. A server-side successful send is not recorded as proof of client receipt.
8. Expiration is evaluated against deadlines even when cleanup has not run.
9. Worker recovery records are durable before asynchronous acknowledgement.
10. No local citizen provisioning is introduced by a convenient consent or IdP SDK default.

## Conformance plan before a real implementation

| Contract | Meaningful checks |
|---|---|
| Request | Canonical digest, changed-scope mismatch, caller-scoped idempotency, limits |
| AuthN | Issuer/audience/signature, nonce/state/PKCE, replay, rotation, provider outage |
| Verification gate | Missing/mismatched context fails; mock profile cannot run as production |
| Consent | Exact notice provenance, duplicate decision, concurrent withdrawal/activation, inaccessible receipt |
| AuthZ | Wrong purpose/fields/recipient, expired context, consumer disabled, policy version |
| Redemption | Competing attempts, multiple JTIs, withdrawal race, recovery of ambiguous result |
| Adapter | Extra-field leakage, malformed source data, source credentials, independent audience validation |
| Evidence | Commit outage, duplicate event, ambiguous acknowledgement, privacy-safe commitments |
| Lifecycle | Reordered callbacks, process restart, deadline worker, terminal cleanup window |
| UI | Keyboard use, accessible language, reject parity, accurate pending/release messages |

These checks define conformance expectations for the implementation. Example flows illustrate the protocol and do not imply production certification.
