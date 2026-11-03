---
title: Protocol and HTTP interfaces
description: Versioned GRAMStack implementation contracts and conformance requirements.
---

Specification version **0.2.0** · [Specifications index](/specifications/) · [Download source](https://docs.gramstack.try-dough.com/static/specs/0.2.0/protocol.md)

# Protocol and HTTP interfaces

## One disclosure, one immutable request

The consumer authenticates with Hydra client credentials and submits `POST /v1/access-requests` with an Idempotency-Key. The gateway verifies the registered consumer, configured registry/resource, exact requested fields, purpose and recipient terms before returning 202, a status URL and a browser interaction URL. Human authentication, subject verification and consent continue asynchronously. Consumer status polling is synchronous and returns only workflow metadata. The default human interaction deadline is ten minutes; requested access validity is up to five minutes after approval, bounded by configured deadlines. These are separate clocks.

An accepted request snapshot contains `request_id`, `consumer_id`, `created_at`, `expires_at` and the submitted `request`. The gateway resolves recipient terms to an immutable `recipient_terms_digest` in that snapshot. Its `request_digest` is SHA-256 over UTF-8 RFC 8785 canonical JSON of this snapshot, excluding the digest itself. Arrays preserve order; field names MUST be unique. All participants use the same snapshot. The external `subject_hint` is only a locator, never proof of ownership. No endpoint accepts a caller-supplied consumer identity as authentication.

All digests use `sha256:` followed by 64 lowercase hexadecimal characters. Simplified digest labels in architecture walkthroughs are explanatory shorthand. Timestamps use RFC 3339 UTC. Identifiers are opaque and must not encode civil identity. JSON request bodies reject unknown properties. Deployments set body-size and rate limits. Error responses contain a safe code/message, request correlation ID and retryable boolean; no raw IdP claims, source rows, tokens or subject identifiers.

## Authentication and subject verification

Hydra owns OAuth/OIDC endpoints. GRAMStack AuthN handlers and IdP adapters implement the integration inside the backend; they are not an additional identity provider or a separately named login application. Use authorization code with PKCE, state and nonce, exact redirect URI checks, and provider issuer/audience/signature/time verification. SAML deployments enforce equivalent assertion, audience, destination and replay checks. Provider metadata and keys come from configured trust anchors, never request-supplied URLs.

A short-lived PrincipalContext stores the validated provider issuer/subject and authentication transaction reference. It is private transaction state, never a global person directory. Registry-specific subject verification yields a VerifiedSubjectContext scoped to the request digest, registry, expiry and verification method. Only a trusted registry-approved verifier may create it. Authentication success and a subject hint cannot substitute for this verification. The source mapping is held behind an opaque reference with a bounded TTL.

The first-party browser uses Secure, HttpOnly, SameSite cookies and CSRF tokens on mutations. Interaction URLs expire and do not authorize consent by themselves. Browser decisions require the authenticated session bound to that interaction and verified subject. `GET /auth/callback` parameters and Hydra `/oauth2/token` are defined by their standard protocols and provider integration, not redefined as GRAMStack JSON messages.

## HTTP behavior

`openapi.json` declares paths relative to the corresponding service's origin. Gateway routes are `/v1/*`, `/interactions/*` and `/consents/*`; consent, AuthZ, evidence and adapter operations are internal routes and MUST NOT become public merely because they share a specification file. Component routing is stated in each operation description. Internal service identity is deployment-pinned mTLS, independent of a retrieval grant.

Idempotency keys are scoped to authenticated actor + operation + request body digest. Repeating the same key and body returns the original operation metadata; a different body returns 409. Retain the record through the request expiry and configured retry window. No idempotency record contains released business data. Use a fresh key only for a genuinely new request.

Requests return 400 for invalid shape, 401 for failed authentication, 403 for forbidden scope, privacy-safe 404 for unknown/non-owned objects, 409 for state/idempotency conflicts, 410 for expired interaction/grant, 429 with Retry-After for rate limits, and 503 for unavailable dependencies. `retryable` does not authorize replay of a consumed disclosure.

## Data delivery

`GET /v1/access-requests/{id}/data` consumes a retrieval grant; it is not a safe polling endpoint. Clients MUST disable prefetch, speculative fetching, shared caching and automatic retries. Responses use Cache-Control: no-store. The authenticated presenter MUST match the grant confirmation thumbprint. Forwarding through the gateway does not remove the adapter's independent verification obligation. The gateway sends the original grant over authenticated service transport and only trusted, request-bound presenter context; source release never relies on an untrusted forwarded client ID.

A 200 contains only authorized fields plus the access receipt reference. If transport fails after redemption, the grant remains consumed and the consumer receives no automatic resend. Check request status/receipt and obtain a new approval when another disclosure is needed.

## Standards

[OpenAPI 3.1](https://spec.openapis.org/oas/v3.1.1.html), [JSON Schema 2020-12](https://json-schema.org/draft/2020-12), [RFC 8785 canonical JSON](https://www.rfc-editor.org/rfc/rfc8785), [OAuth security BCP](https://www.rfc-editor.org/rfc/rfc9700), [mTLS sender constraints](https://www.rfc-editor.org/rfc/rfc8705). This release uses the mTLS grant-confirmation profile; a DPoP profile requires its own specified verification and forwarding behavior before use.

## Explicit cancellation

POST /v1/access-requests/{id}/cancel authenticates the owning consumer and accepts no scope mutation. Before admission it atomically closes the request as CANCELLED and invalidates any pending/approved/active consent under the same lock as redemption. A repeated cancellation returns the same status/version. A guessed or non-owned ID returns privacy-safe 404. A consumed reservation returns 409 with ADMISSION_ALREADY_OCCURRED and cannot be reset, even if the workflow projection still shows an earlier state. Other terminal outcomes remain unchanged and return conflict. Late callbacks cannot resurrect CANCELLED. A client changing scope cancels the old request then submits a new request, new idempotency key and fresh notice/decision. There is no partial-refusal/revision object. Internal cancellation events distinguish the consumer actor from a subject withdrawal.

[Repository source](https://github.com/theflywheel/GRAMStack/tree/main/specs/0.2.0/protocol.md).
