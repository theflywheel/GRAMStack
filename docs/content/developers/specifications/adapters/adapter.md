---
title: Independently hosted registry adapters
description: Versioned GRAMStack implementation contracts and conformance requirements.
---

Specification version **0.3.0** · [Specifications index](/developers/specifications) · [Download source](https://docs.gramstack.try-dough.com/static/specs/0.3.0/adapter.md)

# Independently hosted registry adapters

## Distribution and discovery

An adapter is a separately publishable service implementing this HTTP contract. It may use any language. A Go helper package can assist implementers but is optional; the gateway MUST NOT import registry implementations or choose code through hardcoded registry cases. Registry-specific source access and subject verification stay behind adapter/verifier contracts.

An operator installs an approved descriptor mapping registry ID to adapter HTTPS origin, audience, capability version, service identity and signing keys. GET `/.well-known/gramstack-adapter` returns AdapterManifest. The gateway checks it against that descriptor. Discovery is not permission to trust arbitrary hosts, redirect destinations, keys or resources; disallow user-controlled URLs, private-network SSRF and redirects outside approved origins. The manifest does not expose people or source lookup results.

The manifest lists supported resource names and fields, verification methods and signing key IDs, contract version and authentication profile. This release requires `mtls`, evidence-before-release and atomic-redemption. Unsupported contract or resource versions fail closed. Source credentials remain adapter-local secrets.

## Retrieval processing

POST `/internal/v1/retrievals` requires gateway service mTLS and RetrievalRequest. The adapter validates the signed grant, trusted request snapshot/digest, audience, source resource and exact fields, and obtains trusted presenter context from the authenticated gateway. It resolves the request-scoped verified subject reference using the configured verifier; it never accepts a raw source record ID as proof.

Processing order is: validate → atomically claim local attempt → redeem through AuthZ → fetch at most once → map source fields → project exactly authorized fields → sign evidence → obtain durable evidence acknowledgement → release projected data and receipt reference. Adapter attempt persistence coordinates concurrent duplicates and survives restarts. A duplicate attempt returns 409 with attempt metadata, never a second source read or cached business response.

LokOS demo resource `shg.membership` exposes `membership_status` and `savings_summary`. The latter is `{amount, currency}` with amount expressed in INR rupees, not paise, and currency fixed to INR. It is a source-reported summary, not a computed credit score. Mapping and completeness rules belong to the registry-specific implementation and must be documented against the real source contract. Unsupported or unmappable fields fail; never fabricate a value. Source rows and unrelated fields MUST NOT leak through error messages, evidence or responses.

## Hosting and ownership

Adapters own upstream API integration, credentials, mapping and request-scoped source lookup. Core owns lifecycle, consent and policy enforcement. The registry approves subject verification; the adapter enforces that approval at retrieval. Mock source services live separately under `mocks/`, outside core `cmd/` and outside published production adapter packages. A reference adapter is independently runnable and must pass the same conformance scenarios as third-party adapters.

The core Go backend, optional SDK, reference adapter and mock sources are distinct deliverables; this contract does not require their code to exist inside the registry catalogue. Registry onboarding changes configuration plus the independently deployed adapter, not the gateway binary.

[Repository source](https://github.com/theflywheel/GRAMStack/tree/main/specs/0.3.0/adapter.md).
