---
title: 05 · Authorization and grants
description: Policy inputs, grant claims, atomic redemption and the withdrawal race.
date: 2026-11-01
---
# Authorization and retrieval grants

## Decision inputs

Consent is necessary for the normal profile but never sufficient. AuthZ considers the authenticated consumer, verified subject context, exact request/consent binding, trusted consent issuer, registry policy, fields, purpose, recipient, assurance, deadlines, withdrawal and usage state.

```mermaid
graph TD
    C["Consumer authority"] --> Z["AuthZ decision"]
    V["Verified registry subject"] --> Z
    S["Signed consent and current status"] --> Z
    P["Versioned registry policy"] --> Z
    R["Request digest and deadlines"] --> Z
    Z --> D{"All constraints satisfied?"}
    D -->|No| N["Deny; safe reason and decision evidence"]
    D -->|Yes| G["Short-lived request-scoped grant"]
```

The v1 policy model uses a signed, versioned per-registry bundle evaluated through a small deterministic decision interface. Request-time status, assurance and validity dominate authorization. Registry relationship hierarchies remain source-owned rather than being replicated into a central relationship engine. Revisit a policy engine such as Cedar/OPA only when concrete policy complexity justifies it. 

## Policy shape

```json
{
  "registry": "lokos",
  "version": "lokos.v1",
  "consumer_id": "sakhi-credit",
  "purpose": "credit_eligibility",
  "resource": "shg.membership",
  "fields": ["membership_status", "savings_summary"],
  "require_verified_subject": true,
  "require_human_consent": true,
  "max_grant_seconds": 60,
  "max_disclosures": 1
}
```

Each policy is signed with the source’s approved key using `typ=gramstack-policy+jws`. SQL retains immutable versions and the current enabled head for each registry/consumer/resource/purpose tuple. A replacement requires the expected current version. A retired version cannot be restored, and restarting with the same signed file cannot undo a disable. Registry profiles additionally bind issuer trust, recipient terms, supported assurance and canonical resource semantics. Unknown policy fields fail closed.

## Grant claims

```json
{
  "iss": "https://authz.gramstack.example",
  "aud": "urn:gramstack:adapter:lokos",
  "sub": "request-scoped-subject-reference",
  "client_id": "sakhi-credit",
  "jti": "grant_73c2",
  "iat": 1793527320,
  "exp": 1793527620,
  "request_id": "req_8f21",
  "request_digest": "sha256:request-A",
  "resource": "shg.membership",
  "fields": ["membership_status", "savings_summary"],
  "purpose": "credit_eligibility",
  "consent_ref": "con_61b4",
  "subject_context_ref": "ctx_77",
  "policy_version": "lokos.v1",
  "cnf": {"x5t#S256": "registered-consumer-certificate-SHA256-base64url"}
}
```

AuthZ uses a signed grant with a separate trusted issuer and JWKS. Expiry is bounded by consent validity, subject-context validity, request deadline and registry policy. Claims contain authorization metadata, not registry response data. JWT payloads are readable unless separately encrypted, so avoid raw identifiers and unnecessary attributes.

The selected profile uses mTLS sender constraints. At issuance, Hydra authenticates the consumer and AuthZ binds `cnf.x5t#S256` to its registered certificate. Retrieval additionally verifies possession of that certificate. The gateway and adapter convey presenter context only over authenticated service transport; the adapter’s private redemption call attests the verified original presenter. AuthZ verifies the adapter certificate owns the audience and matches the attested presenter to the grant. A forwarded identity header alone is never proof. DPoP would require its own defined proof-verification profile.

## Issuance and one-time redemption

Grant issuance is an explicit consumer call after consent. Store/recover a stable issuance result so retries do not mint unlimited distinct grants. Existing issuance retries return the same signed bytes without requiring the signer to be online, while still checking current authority; expiry never extends. A one-time consent cannot be bypassed by creating a second `jti`.

The gateway performs perimeter validation, but the adapter is the redemption enforcement point. It independently validates issuer, signature, audience, time, request, resource and fields, then calls authenticated internal AuthZ redemption. Only the authoritative atomic reservation permits a source read.

```mermaid
graph TD
    G["Gateway validates and dispatches"] --> A["Adapter validates independently"]
    A --> Z["Authenticated redemption command"]
    Z --> T["Atomic consent and grant enforcement transaction"]
    T --> R{"Consent ACTIVE, grant unused, one-use AVAILABLE?"}
    R -->|Yes| W["Consume grant and reserve consent use"]
    R -->|No| D["Deny without source access"]
    W --> S["Fetch and filter source response"]
```

Redemption also binds the adapter identity, request digest and a stable retrieval attempt ID. The same authenticated attempt recovers its reservation metadata with `first_redemption=false`, even after withdrawal, policy revocation or grant expiry. That response acknowledges a historical admission. It never grants another source read or data release. A different attempt conflicts.

## Withdrawal and atomic enforcement

A status read followed by an independent Redis `SET NX` has a race: ACTIVE may be read before withdrawal, then redemption may succeed afterward. Single-use token replay prevention alone is insufficient.

Authoritative consent enforcement and grant redemption share one transactional SQL boundary. Consent and AuthZ remain separate modules with controlled writes. Gateway TTL state and Hydra SQL have separate responsibilities.

```text
BEGIN
  Lock request, current source policy head, then consent enforcement record
  Validate consent status, version, expiry and one-use availability
  Validate grant, consumer, adapter and request binding
  Insert unique grant redemption
  Reserve the consent's single permitted use for attempt_id
  Record enforcement event and durable work item
COMMIT
```

Withdrawal locks/checks the same consent enforcement record. Whichever transaction commits first determines whether retrieval is admitted. This defines the retrieval admission cutoff; it does not promise cancellation up to the final response byte. A requirement to cancel after admission needs additional cancellation/release coordination and a precisely defined final cutoff.

An equivalent atomic store can implement the enforcement contract. Independent stores require a coordination protocol that preserves the same consistency guarantees. See [state](/architecture/08-state-and-evidence).

## After redemption

The one-use profile is conservative: the grant remains consumed after source failure, evidence failure or ambiguous timeout. Do not reauthorize automatically or cache a response to make retries easy. A fresh disclosure request and approval may be required. This preserves one-use semantics even when a failure requires the user to authorize a new attempt.

Record failed, attempted and uncertain outcomes separately. A server completing transmission is not proof of complete client receipt. Evidence of attempted disclosure and a spent grant remain valid even when the consumer reports a timeout.

## Versioned grant profile

The [0.1.0 authorization contract](/developers/specifications/core/authorization) specifies ES256 signed grants and an mTLS presenter-binding profile. The consumer certificate is checked on the retrieval transport, and trusted presenter context crosses authenticated service transport to the adapter. DPoP requires a separately specified proof-verification profile.

## Synchronous calls and durable authority

```mermaid
sequenceDiagram
    participant C as Consumer backend
    participant G as Gateway / AuthZ
    participant P as SQL authority
    participant A as Registry adapter
    C->>G: POST request/grant + Hydra token + idempotency key
    G->>P: Read request, current signed policy, ACTIVE consent and context
    Note over G: Verify bindings and synchronously sign the bounded grant
    G->>P: Lock and recheck authority, persist grant + FSM + outbox
    P-->>G: Commit
    G-->>C: Same scoped grant on valid retry
    C->>G: Retrieval over authenticated presenter transport
    G->>A: Grant + verified presenter context over service mTLS
    A->>A: Independently verify grant and scope
    A->>G: Private mTLS redemption with stable attempt ID
    G->>P: Lock request, policy and consent, reserve use + FSM + outbox
    P-->>G: Commit one reservation
    G-->>A: first_redemption and reservation metadata
    Note over A: Durable SDK attempt controls the subsequent fetch and evidence gate
```

Issuance and admission are synchronous. Both persist authority and metadata outbox events before responding; asynchronous notification delivery cannot grant or restore authority. SQL stores signed policies, current policy heads, immutable grants and reservation metadata. The grant contains no source response. The private consent-status endpoint is restricted to the adapter audience associated with the issued grant and is informational; a status read cannot replace atomic admission. Public JWKS contains only verification keys, and clients pin its trusted issuer/origin rather than follow a token-provided URL.
