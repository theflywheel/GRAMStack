---
title: 07 · API contracts
description: Public, browser and internal APIs with payloads and processing rules.
date: 2026-11-03
---
# API contracts

> [!info] API conventions
> This catalogue describes GRAMStack-facing interfaces. Payloads use illustrative identifiers and values. Native registry endpoints remain behind their adapters; subject-verification mechanisms follow each registry's approved integration profile.

## Consumer-facing APIs

| Method and route | Authentication | Behavior |
|---|---|---|
| `POST /oauth2/token` | Registered consumer client authentication | Hydra consumer token; institutional identity only |
| `POST /v1/access-requests` | Consumer token + idempotency key | Validate, persist, return 202 and interaction/status locators |
| `GET /v1/access-requests/{id}` | Owning consumer token | Current safe status and next action; no private verification candidates |
| `POST /v1/access-requests/{id}/cancel` | Owning consumer token | Idempotently cancel unadmitted authority; 409 after admission or incompatible terminal state (contract v0.2) |
| `POST /v1/access-requests/{id}/grant` | Owning consumer token + idempotency key | Evaluate consent/context/policy; return grant or safe denial |
| `GET /v1/access-requests/{id}/data` | Retrieval grant plus defined presenter authentication | Single admitted retrieval; evidence before release; no-store |

The retrieval endpoint consumes disclosure authority. Clients must not prefetch or automatically retry it. An integration can expose an explicit POST retrieval command with equivalent enforcement semantics where required by client infrastructure. No generic public member-by-ID lookup is implied.

## Create access request

```http
POST /v1/access-requests
Authorization: Bearer <consumer-token>
Idempotency-Key: sakhi-application-482
Content-Type: application/json
```

```json
{
  "registry": "lokos",
  "resource": "shg.membership",
  "subject_hint": {"type": "member_reference", "value": "member-12345"},
  "fields": ["membership_status", "savings_summary"],
  "purpose": "credit_eligibility",
  "access": {"mode": "single_use", "valid_for_seconds": 300},
  "recipient_terms": {"retention_policy_id": "sakhi-retention-v1"}
}
```

The server derives consumer and recipient identity from trusted client configuration. Validate registry/resource/field allowlists, purpose, terms version, size limits and deadline policy. Treat hints as sensitive untrusted lookup inputs. Scope the idempotency key to caller and operation and associate it with a canonical request digest.

```http
HTTP/1.1 202 Accepted
Location: /v1/access-requests/req_8f21
Cache-Control: no-store
```

```json
{
  "request_id": "req_8f21",
  "status": "AUTHENTICATION_PENDING",
  "status_url": "/v1/access-requests/req_8f21",
  "interaction_url": "https://consent.example/interactions/int_35",
  "expires_at": "2026-11-01T10:10:00Z"
}
```

Same key and equivalent input returns the prior result within the defined retention window. Same key with changed input returns 409. A locator never grants authority; all subsequent access has caller/session checks.

## Status and errors

```json
{
  "request_id": "req_8f21",
  "status": "CONSENTED",
  "version": 6,
  "next_action": "REQUEST_GRANT",
  "expires_at": "2026-11-01T10:10:00Z"
}
```

```json
{
  "error": {
    "code": "VERIFICATION_UNSUCCESSFUL",
    "message": "We could not complete verification for this request.",
    "request_id": "req_8f21",
    "retryable": false
  }
}
```

Transport handling: 400 invalid schema, 401 invalid caller authentication, 403 policy/authority denial, 404 inaccessible/not-found without enumeration, 409 state/idempotency conflict, 410 expired interaction where safe, 429 throttled and 503 unavailable mandatory dependency. Separate stable public codes from internal upstream diagnostics. HTTP status is not the workflow's authoritative terminal outcome by itself.

## Browser interaction APIs

| Method and route | Processing |
|---|---|
| `GET /interactions/{id}` | Resolve authorized interaction, authenticate or resume |
| `GET /auth/callback` | Validate first-party OIDC transaction and rotate session |
| Provider-specific callbacks | Validate upstream protocol, correlation and trust |
| `POST /interactions/{id}/verify` | Request-bound session, exact Origin and form CSRF; registry verification, no browser-supplied principal or mapping |
| `GET /interactions/{id}/consent` | Render immutable request and exact notice snapshot |
| `POST /interactions/{id}/decision` | Session/principal/context and CSRF checks; record decision idempotently |
| `POST /consents/{id}/withdrawal` | Reauthenticate/authorize subject; enforce atomic cutoff |
| `GET /interactions/{id}/receipt` | Authorized view of notice, artefact and safe release outcome |

Withdrawal targets the consent resource. Later review and withdrawal require authorized reauthentication and request correlation. Return URLs are allowlisted, never arbitrary redirects.

```http
POST /interactions/int_35/decision
Cookie: <secure-http-only-session>
X-CSRF-Token: <session-bound-value>
Content-Type: application/json
```

```json
{
  "decision": "approve",
  "request_digest": "sha256:request-A",
  "notice_digest": "sha256:notice-HI-v1",
  "notice_version": "consent-notice-v1",
  "language": "hi"
}
```

The server compares browser values with the notice actually presented. It derives principal and registry context server-side. The response distinguishes APPROVAL_RECORDED from ACTIVE readiness.

## Registry-approved subject verification

The independently versioned [subject-verification profile 0.1.0](/specifications/subject-verification) defines the private gateway-to-adapter exchange alongside core contract 0.2.0. A trusted descriptor pins the HTTPS origin, registry, server certificate identity, approved method and `verification_contract_version`. Both sides require the matching `Gramstack-Subject-Verification-Version` header. Unsupported contracts fail closed.

```mermaid
sequenceDiagram
  participant Person as Sunita browser
  participant Gateway as Gateway and AuthN handlers
  participant Adapter as SDK-backed registry adapter
  participant Native as Registry source verifier
  participant AdapterDB as Adapter PostgreSQL
  participant CoreDB as Gateway PostgreSQL
  Person->>Gateway: POST interaction verify with session and CSRF
  Gateway->>Gateway: Load accepted request and verified principal
  Gateway->>Adapter: POST subject-verifications over mTLS with pinned version
  Adapter->>Adapter: Recompute digest and validate scope and freshness
  Adapter->>Native: Check approved identity-to-record relationship
  Native-->>Adapter: Opaque mapping reference or safe denial
  Adapter->>AdapterDB: Persist encrypted request-scoped context
  Adapter-->>Gateway: Verified context bounded by request and authentication
  Gateway->>CoreDB: Transaction: recheck state, save context, advance FSM, append outbox
  alt Cancelled, expired, or write failed
    CoreDB-->>Gateway: Roll back and deny or report unavailable
    Gateway-->>Person: Safe failure; no consent or data
  else Verification committed
    CoreDB-->>Gateway: CONSENT_PENDING
    Gateway-->>Person: Opaque context reference and review-consent action
  end
```

The verification interaction waits synchronously for its outcome. The consumer’s accepted workflow remains asynchronous and pollable. Network verification holds no core SQL lock; cancellation and deadlines are checked again when committing. Context, FSM transition and outbox event share the transaction. Outbox delivery is asynchronous. Identical live retries converge on one context; a late result cannot revive cancelled authority.

The browser sends only form field `csrf`; principal and accepted request are loaded server-side. The private adapter request carries `{snapshot, request_digest, principal}`. Its response is a VerifiedSubjectContext, not membership or savings data. Verified native mappings remain source-owned; only opaque mapping handles are returned across the adapter boundary. Both adapter and gateway encrypt their bounded context custody, and database TTL checks remain effective after restart.

## Internal consent APIs

| Method and route | Caller | Behavior |
|---|---|---|
| `POST /internal/consent-requests` | Gateway | Idempotently create immutable consent request using trusted context reference |
| `GET /internal/consent-requests/{id}` | Gateway | Authoritative preparation/decision state for reconciliation |
| `GET /internal/consents/{id}/artefact` | Authorized services | Signed artefact with access-controlled metadata |
| `GET /internal/consents/{id}/status` | AuthZ/Gateway | Current version, validity, withdrawal and usage status |
| `POST /internal/consent-events` | Consent event publisher | Gateway receiver for signed/authenticated events; stable-ID deduplication |

```json
{
  "event_id": "evt_con_61b4_active_1",
  "type": "consent.active",
  "consent_id": "con_61b4",
  "access_request_id": "req_8f21",
  "request_digest": "sha256:request-A",
  "status_version": 1,
  "occurred_at": "2026-11-01T10:02:00Z"
}
```

Events include authenticated issuer and payload integrity in the chosen transport envelope. A duplicate ID with different content is an integrity error. Stale events cannot undo withdrawal.

## Redemption, adapter and evidence APIs

| Method and route | Caller | Behavior |
|---|---|---|
| `POST /internal/v1/grants/redeem` | Authenticated adapter | Atomic consent/grant/use reservation; stable attempt ID |
| `POST {adapter}/internal/v1/retrievals` | Authenticated gateway | Independently enforce grant, fetch/map/filter and gate release |
| `POST /internal/v1/evidence` | Authorized evidence producers | Idempotent signed event commitment and durable acknowledgement |
| `GET /.well-known/gramstack-adapter` | Authorized discovery as policy requires | Capabilities and trust metadata, not personal records |
| Issuer JWKS routes | Verifiers | Trusted keys and bounded rotation; no private key material |

```json
{
  "grant": "<signed-retrieval-grant>",
  "request_id": "req_8f21",
  "request_digest": "sha256:request-A",
  "attempt_id": "attempt_04"
}
```

Consumer tokens, browser sessions, service tokens, grants and evidence signatures are distinct credentials with separate audiences. Internal APIs require service authentication and network restrictions; `internal` in a path is not a security control.

## Registry verification integration

Capability discovery and verification workflows are adapter responsibilities. The gateway consumes a trusted verified context bound to the principal, registry and access request. Native verification mechanisms and assurance profiles are registry-specific; the architecture does not require every registry to operate an IdP.

## Platform extension interfaces

These architecture interfaces expose the additional platform responsibilities described in [consent](04-consent) and [data handling](08-state-and-evidence). These interfaces are separate from the published v0.2 cancellation API; they are not additional implemented wire operations. A versioned capability/schema defines each extension before client adoption.

| Interface | Authorised caller | Contract and timing |
|---|---|---|
| Resolve purpose/processing profile | Gateway and authorised clients | Read approved versions/digests; reject arbitrary URLs and incompatible scope |
| Record presentation evidence | Trusted consent presentation component | Bind exact notice, channel, language and authenticated decision; durable before activation |
| Consent/disclosure history and export | Reauthenticated subject or supported representative | Access-controlled, paginated/minimised history and machine-readable export |
| Create/read rights case | Verified requester and responsible case handler | Durable acceptance and safe status; asynchronous routing/outcome |
| Submit handling receipt | Registered recipient or authorised processor | Authenticate producer, verify disclosure/policy binding and commit idempotently |
| Pause source or suspend consumer | Authorised source owner/operator | Audited current-policy change enforced at issuance and admission |

Cancellation and receipt submission use operation-scoped idempotency keys; same key with changed content conflicts. Rights-case creation acknowledges acceptance, not erasure or correction. Public statuses distinguish pending, acknowledged, in progress, completed, exception and failed without exposing private internal diagnostics. Reliable notifications use authenticated outbox delivery and deduplication; polling/status remains authoritative.

Neither case IDs nor export locators grant access. Browser mutations require session/CSRF protection; service mutations require scoped service credentials. Receipt lookup, export and administrative access are audited. The platform derives organisation/subject authority from trusted context, never from an arbitrary payload identity.


## Versioned implementation contracts

Use the [implementation specifications](specifications/) for the normative API, message and adapter contracts. [OpenAPI](specifications/openapi), [message schemas](specifications/schemas), [adapter requirements](specifications/adapter) and [conformance scenarios](specifications/conformance) are published from the repository’s canonical `specs/` sources.
