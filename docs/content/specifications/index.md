---
title: Implementation specifications
description: Versioned GRAMStack implementation contracts and conformance requirements.
---

# GRAMStack specifications

The versioned contracts for implementing GRAMStack and independently hosted registry adapters. The backend uses Go; the wire contracts are language independent. An adapter implements the HTTP contract and can be published and deployed independently of the gateway. No Go SDK or registry-specific gateway code is required.

Current specification version: **0.3.0**. Versions 0.1.0 and 0.2.0 remain unchanged. Version 0.3.0 adds request withdrawal from CONSENT_PENDING so recorded approval can be stopped before activation, plus deadline-safe reconciliation of already recorded stops. Version 0.2.0 introduced consumer cancellation. API routes use `/v1`; schema and capability versions identify the contract release separately. These contracts describe required behavior; a schema-valid message alone does not establish security or implementation conformance.

## Contract set

| Document | Scope |
|---|---|
| [Protocol](protocol) | Actors, trust, canonical requests, authentication, idempotency and errors |
| [Consent](consent) | Notices, decisions, signed artefacts, withdrawal and events |
| [Authorization](authorization) | Policy, grants, presenter binding and atomic redemption |
| [Adapter](adapter) | Discovery, retrieval, source filtering and independent deployment |
| [Evidence and state](evidence-and-state) | Persistence, lifecycle, receipts and failure semantics |
| [Conformance](conformance) | Required behavioral checks and compatibility rules |
| [OpenAPI](openapi) | Consumer, browser, consent, AuthZ, adapter and evidence HTTP interfaces |
| [JSON Schema](schemas) | Message shapes, signed payloads and capability declarations |
| [Lifecycle](lifecycle) | Allowed state transitions |
| [Examples](examples) | Positive and negative message fixtures |

## Implemented API explorer

[Hosted Swagger UI](https://docs.gramstack.try-dough.com/api/) provides service
catalogues for the public gateway, private gateway mTLS listener, LokOS adapter,
and SakhiCredit product. The [runtime catalogue](/specifications/runtime) describes
their deployment scope. The selector also includes the core 0.3.0 protocol.

## Independently versioned profiles

[Registry subject verification 0.1.0](/specifications/subject-verification) defines the mTLS verifier exchange, bounded principal provenance, context custody and cancellation-safe idempotency. [Canonical profile source](https://github.com/theflywheel/GRAMStack/tree/main/specs/subject-verification/0.1.0). Core 0.1.0 and 0.2.0 remain unchanged.

[First-party consent portal 0.1.0](/specifications/consent-portal) defines exact WEB/English presentations, session-bound form decisions, withdrawal and protected read-only exports. [Canonical profile source](https://github.com/theflywheel/GRAMStack/tree/main/specs/consent-portal/0.1.0).

[Adapter discovery 0.1.0](/specifications/adapter-discovery) defines public capability/JWK metadata and fail-closed comparison against approved origins, methods, resources and key material. [Canonical profile source](https://github.com/theflywheel/GRAMStack/tree/main/specs/adapter-discovery/0.1.0).

[Post-disclosure 0.1.0](/specifications/post-disclosure) defines protected history and receipt recovery, authenticated recipient handling assertions and responsible-party rights case routing.

## Maintaining the contracts

Edit this directory; Quartz generates its specification pages and downloadable files from it. Do not edit generated copies under `docs/content/specifications/`. Run `cd docs && npm ci && npm run verify:specs && npm run build && npm run verify:content`.

Changes to required fields, meaning, authorization behavior or allowed transitions require a new specification version. Unknown security-sensitive input fields are rejected. Optional additive capabilities require explicit negotiation; a recipient must never silently widen fields or purpose. Keep published releases immutable once implementations target them.

Normative words MUST, MUST NOT and SHOULD indicate requirements. Examples are fictional and carry no valid signature or access authority.

[Canonical repository source](https://github.com/theflywheel/GRAMStack/tree/main/specs).
