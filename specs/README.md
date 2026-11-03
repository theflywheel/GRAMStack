# GRAMStack specifications

The versioned contracts for implementing GRAMStack and independently hosted registry adapters. The backend uses Go; the wire contracts are language independent. An adapter implements the HTTP contract and can be published and deployed independently of the gateway. No Go SDK or registry-specific gateway code is required.

Current specification version: **0.3.0**. Versions 0.1.0 and 0.2.0 remain unchanged. Version 0.3.0 adds request withdrawal from CONSENT_PENDING so recorded approval can be stopped before activation, plus deadline-safe reconciliation of already recorded stops. Version 0.2.0 introduced consumer cancellation. API routes use `/v1`; schema and capability versions identify the contract release separately. These contracts describe required behavior; a schema-valid message alone does not establish security or implementation conformance.

## Contract set

| Document | Scope |
|---|---|
| [Protocol](0.3.0/protocol.md) | Actors, trust, canonical requests, authentication, idempotency and errors |
| [Consent](0.3.0/consent.md) | Notices, decisions, signed artefacts, withdrawal and events |
| [Authorization](0.3.0/authorization.md) | Policy, grants, presenter binding and atomic redemption |
| [Adapter](0.3.0/adapter.md) | Discovery, retrieval, source filtering and independent deployment |
| [Evidence and state](0.3.0/evidence-and-state.md) | Persistence, lifecycle, receipts and failure semantics |
| [Conformance](0.3.0/conformance.md) | Required behavioral checks and compatibility rules |
| [OpenAPI](0.3.0/openapi.json) | Consumer, browser, consent, AuthZ, adapter and evidence HTTP interfaces |
| [JSON Schema](0.3.0/schemas.json) | Message shapes, signed payloads and capability declarations |
| [Lifecycle](0.3.0/lifecycle.json) | Allowed state transitions |
| [Examples](0.3.0/examples.json) | Positive and negative message fixtures |

## Independently versioned profiles

[Registry subject verification 0.1.0](/specifications/subject-verification) defines the mTLS verifier exchange, bounded principal provenance, context custody and cancellation-safe idempotency. [Canonical profile source](https://github.com/theflywheel/GRAMStack/tree/main/specs/subject-verification/0.1.0). Core 0.1.0 and 0.2.0 remain unchanged.

[First-party consent portal 0.1.0](/specifications/consent-portal) defines exact WEB/English presentations, session-bound form decisions, withdrawal and protected read-only exports. [Canonical profile source](https://github.com/theflywheel/GRAMStack/tree/main/specs/consent-portal/0.1.0).

## Maintaining the contracts

Edit this directory; Quartz generates its specification pages and downloadable files from it. Do not edit generated copies under `docs/content/specifications/`. Run `cd docs && npm ci && npm run verify:specs && npm run build && npm run verify:content`.

Changes to required fields, meaning, authorization behavior or allowed transitions require a new specification version. Unknown security-sensitive input fields are rejected. Optional additive capabilities require explicit negotiation; a recipient must never silently widen fields or purpose. Keep published releases immutable once implementations target them.

Normative words MUST, MUST NOT and SHOULD indicate requirements. Examples are fictional and carry no valid signature or access authority.
