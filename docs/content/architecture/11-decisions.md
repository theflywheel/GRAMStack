---
title: 11 · Architecture choices
description: The architectural choices behind federated identity, consent-first retrieval, registry adapters and verifiable evidence.
date: 2026-11-03
---
# Architecture choices

## Core design

| Concern | Architectural choice | Rationale |
|---|---|---|
| Source authority | Business data remains in existing registries | Avoid stale replicas and preserve source ownership |
| Citizen identity | Delegate authentication to external authorities | Avoid local citizen accounts and universal identity mappings |
| Scope | Read-only registry access | Keep source-record mutation outside the retrieval protocol |
| Coordination | Gateway-owned request lifecycle | Support asynchronous human interaction without requiring a standalone orchestrator |
| Integration | Registry adapters own transport, verification integration, transformation and enforcement | Keep source-specific behavior behind one executable boundary |
| Human approval | Explicit consent bound to the exact request and verified subject | Prevent substitution of fields, purpose, recipient or person |
| Authentication engine | Hydra with external login integrations | Reuse OAuth/OIDC protocol handling without citizen account provisioning |
| Consent | A dedicated consent module informed by DEPA and open implementations | Preserve notice provenance, withdrawal, signed artefacts and reliable events |
| Authorization | GRAMStack AuthZ evaluates versioned registry policies | Keep policy and retrieval capability separate from login and consent |
| Retrieval grants | Short-lived grants with audience, resource, fields, purpose and request binding | Constrain disclosure authority and its use |
| One-time use | Atomic consent-use reservation and grant redemption | Prevent replay and coordinate withdrawal with retrieval admission |
| Evidence | Durable signed evidence before protected release, anchored through DeDi | Make GRAMStack-mediated access accountable without storing business payloads |
| Credentials | Citizen credentials remain upstream | Avoid local password, recovery and passkey enrollment stores |
| Optional registry IdP | eSignet can sit upstream for a registry | Support registry-owned authentication without mandating it for onboarding |

## Registry subject verification

Subject verification establishes the relationship between an authenticated principal and a source-system record. The integration contract is hybrid: common trusted context, request binding and assurance metadata, with verification mechanics controlled by the registry.

A registry can support its own member IdP, an external identity assertion plus a registry challenge, or an approved assisted procedure. An operator account that can manage many members is not evidence that the operator owns every member's record. No registry is required to operate an IdP.

A verified context contains the authorized issuer, opaque registry subject, intended audience, authenticated principal reference, request digest, method, assurance, freshness and evidence reference. The gateway accepts it only under the registry's trust policy. A lookup hint, phone number or passbook number alone cannot satisfy this gate.

## Deployment profiles

| Profile dimension | Contract |
|---|---|
| Verification | Registry-approved method, issuer trust, assurance and failure handling |
| Consent | Versioned notice and artefact schema; authenticated decision and withdrawal |
| Sender binding | Defined mTLS/DPoP or equivalent presenter-authentication requirements at each relevant hop |
| Evidence anchoring | Synchronous DeDi commitment, or durable custody with bounded batched anchoring |
| Retention | Explicit operational cleanup and evidence custody policy, including backups |
| Runtime isolation | Module co-location or service separation according to key, credential and availability boundaries |
| Canonical data | Resource semantics, field mappings, source freshness and supported operations |

Profiles must preserve the same core guarantees. An integration cannot relax subject verification, human consent, scope enforcement or mandatory evidence by treating these as optional deployment details.

## Channel-independent platform contracts

| Concern | Architectural choice |
|---|---|
| Product logic | Voice/AI interpretation, credit/advisory decisions and conversational state remain outside the platform |
| Fixed scope | Approve/reject the exact request; clients cancel and create a new request when scope changes |
| Purpose and terms | Bind trusted, versioned purpose/processing snapshots and supported time bounds to the immutable request |
| Presentation | Retain exact notice and channel/language confirmation evidence separately from authority |
| Assistance and representation | Assistance confers no authority; representative profiles require explicit verified scope |
| Downstream handling | Bind recipient obligations and record authenticated handling assertions without claiming guaranteed deletion |
| Rights | Expose protected history/export and durable routing/status for responsible organisations |
| Multi-source products | Independent per-source requests; product correlation never broadens authority |
| Protocol evolution | Version and negotiate extensions; preserve the published v0.1 schema and adapter enforcement |


## Consistency boundaries

Gateway workflow state is a projection of progress; it cannot override authoritative consent withdrawal or grant consumption. Consent enforcement and redemption share a transaction boundary. A callback, cached status or delayed outbox event does not independently authorize a source read.

Asynchronous work uses stable IDs, durable work items, acknowledgements, deduplication and reconciliation. State-changing retries recover prior results rather than creating new disclosure authority. After admission, uncertain failures do not reset grants to unused.

## Extensibility

IdP adapters normalize verified authentication context while retaining provider assurance provenance. Registry adapters implement source transport and mappings while enforcing the canonical scope. Consent artefacts identify their issuer and schema so that trusted external consent providers can be integrated through explicit trust and conformance contracts.

These extension points do not require a central citizen registry, mirrored operator hierarchy, business-data cache or separate service for every logical responsibility.
