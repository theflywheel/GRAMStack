---
title: Specifications
description: Versioned GRAMStack implementation contracts and conformance requirements.
---

Choose a contract family: [Core protocol](/developers/specifications/core) · [Authentication and consent profiles](/developers/specifications/profiles) · [Adapter contracts](/developers/specifications/adapters) · [Schemas and examples](/developers/specifications/schemas) · [Conformance and versioning](/developers/specifications/conformance)

# GRAMStack specifications

The versioned contracts for implementing GRAMStack and independently hosted registry adapters. The backend uses Go; the wire contracts are language independent. An adapter implements the HTTP contract and can be published and deployed independently of the gateway. No Go SDK or registry-specific gateway code is required.

Current specification version: **0.3.0**. Versions 0.1.0 and 0.2.0 remain unchanged. Version 0.3.0 adds request withdrawal from CONSENT_PENDING so recorded approval can be stopped before activation, plus deadline-safe reconciliation of already recorded stops. Version 0.2.0 introduced consumer cancellation. API routes use `/v1`; schema and capability versions identify the contract release separately. These contracts describe required behavior; a schema-valid message alone does not establish security or implementation conformance.

## Contract set

| Document | Scope |
|---|---|
| [Protocol](/developers/specifications/core/protocol) | Actors, trust, canonical requests, authentication, idempotency and errors |
| [Consent](/developers/specifications/core/consent) | Notices, decisions, signed artefacts, withdrawal and events |
| [Authorization](/developers/specifications/core/authorization) | Policy, grants, presenter binding and atomic redemption |
| [Adapter](/developers/specifications/adapters/adapter) | Discovery, retrieval, source filtering and independent deployment |
| [Evidence and state](/developers/specifications/core/evidence-and-state) | Persistence, lifecycle, receipts and failure semantics |
| [Conformance](/developers/specifications/conformance) | Required behavioral checks and compatibility rules |
| [OpenAPI](/developers/specifications/schemas/openapi) | Consumer, browser, consent, AuthZ, adapter and evidence HTTP interfaces |
| [JSON Schema](/developers/specifications/schemas/messages) | Message shapes, signed payloads and capability declarations |
| [Lifecycle](/developers/specifications/schemas/lifecycle) | Allowed state transitions |
| [Examples](/developers/specifications/schemas/examples) | Positive and negative message fixtures |

## Implemented API explorer

[Hosted Swagger UI](https://gramstack-gateway-b9b69819.up.railway.app/swagger/) provides the gateway public and private catalogues. Each service hosts its own UI.
[LokOS adapter Swagger](https://gramstack-adapter-b9b69819.up.railway.app/swagger/)
and [SakhiCredit Swagger](https://sakhicredit-b9b69819.up.railway.app/swagger/)
serve their respective API contracts. The [runtime catalogue](/developers/api-reference/catalogue) describes
their deployment scope. The selector also includes the core 0.3.0 protocol.

[Standalone application authentication 0.1.0](/developers/specifications/profiles/application-auth) defines enrolled OpenID clients, pairwise identities and isolated sign-in. [Application SDKs](/developers/sdks) cover Go, Python and JavaScript.

## Independently versioned profiles

[Registry subject verification 0.1.0](/developers/specifications/profiles/subject-verification) defines the mTLS verifier exchange, bounded principal provenance, context custody and cancellation-safe idempotency. [Canonical profile source](https://github.com/theflywheel/GRAMStack/tree/main/specs/subject-verification/0.1.0). Core 0.1.0 and 0.2.0 remain unchanged.

[First-party consent portal 0.1.0](/developers/specifications/profiles/consent-portal) defines exact WEB/English presentations, session-bound form decisions, withdrawal and protected read-only exports. [Canonical profile source](https://github.com/theflywheel/GRAMStack/tree/main/specs/consent-portal/0.1.0).

[Adapter discovery 0.1.0](/developers/specifications/adapters/adapter-discovery) defines public capability/JWK metadata and fail-closed comparison against approved origins, methods, resources and key material. [Canonical profile source](https://github.com/theflywheel/GRAMStack/tree/main/specs/adapter-discovery/0.1.0).

[Post-disclosure 0.1.0](/developers/specifications/profiles/post-disclosure) defines protected history and receipt recovery, authenticated recipient handling assertions and responsible-party rights case routing.

## Maintaining the contracts

Edit this directory; Quartz generates its specification pages and downloadable files from it. Do not edit generated copies under `docs/content/developers/specifications/`. Run `cd docs && npm ci && npm run verify:specs && npm run build && npm run verify:content`.

Changes to required fields, meaning, authorization behavior or allowed transitions require a new specification version. Unknown security-sensitive input fields are rejected. Optional additive capabilities require explicit negotiation; a recipient must never silently widen fields or purpose. Keep published releases immutable once implementations target them.

Normative words MUST, MUST NOT and SHOULD indicate requirements. Examples are fictional and carry no valid signature or access authority.

[Canonical repository source](https://github.com/theflywheel/GRAMStack/tree/main/specs).
