---
title: Implementation specifications
description: Versioned GRAMStack implementation contracts and conformance requirements.
---

# GRAMStack specifications

The versioned contracts for implementing GRAMStack and independently hosted registry adapters. The backend uses Go; the wire contracts are language independent. An adapter implements the HTTP contract and can be published and deployed independently of the gateway. No Go SDK or registry-specific gateway code is required.

Current specification version: **0.1.0**. API routes use `/v1`; schema and capability versions identify the contract release separately. These contracts describe required behavior; a schema-valid message alone does not establish security or implementation conformance.

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

## Maintaining the contracts

Edit this directory; Quartz generates its specification pages and downloadable files from it. Do not edit generated copies under `docs/content/specifications/`. Run `cd docs && npm ci && npm run verify:specs && npm run build && npm run verify:content`.

Changes to required fields, meaning, authorization behavior or allowed transitions require a new specification version. Unknown security-sensitive input fields are rejected. Optional additive capabilities require explicit negotiation; a recipient must never silently widen fields or purpose. Keep published releases immutable once implementations target them.

Normative words MUST, MUST NOT and SHOULD indicate requirements. Examples are fictional and carry no valid signature or access authority.

[Canonical repository source](https://github.com/theflywheel/GRAMStack/tree/main/specs).
