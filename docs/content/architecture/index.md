---
title: Architecture
---

Understand the components, trust boundaries and decisions behind GRAMStack. Read the chapters in order, or jump to the topic you need.

Start with [purpose and scenario](/architecture/01-purpose), then follow the [interactive architecture walkthrough](/architecture/02-architecture#interactive-architecture-walkthrough).

| Chapter | Coverage |
|---|---|
| [01 · Purpose and scenario](/architecture/01-purpose) | Scope, actors, architectural invariants and the example that drives the design. |
| [02 · High-level architecture](/architecture/02-architecture) | Logical components, runtime boundaries, control plane and protected data path. |
| [03 · Authentication](/architecture/03-authentication) | External identities, Hydra integration, IdP adapters and browser transaction security. |
| [04 · Consent manager](/architecture/04-consent) | Request-bound human consent, durable artefacts, withdrawal and implementation boundaries. |
| [05 · Authorization and grants](/architecture/05-authorization) | Policy inputs, grant claims, atomic redemption and the withdrawal race. |
| [06 · Request lifecycle](/architecture/06-lifecycle) | State transitions, asynchronous boundaries, atomic updates and recovery rules. |
| [07 · API contracts](/architecture/07-api-contracts) | Public, browser and internal APIs with payloads and processing rules. |
| [08 · State and evidence](/architecture/08-state-and-evidence) | Custody, retention, stores and evidence-before-release semantics. |
| [09 · Dry runs and user screens](/architecture/09-dry-runs) | Concrete approved, rejected, withdrawn and failed retrievals with storage checkpoints. |
| [10 · Failure and security review](/architecture/10-failures) | Negative cases, race conditions, recovery and conformance requirements. |
| [11 · Architecture choices](/architecture/11-decisions) | The architectural choices behind federated identity, consent-first retrieval, registry adapters and verifiable evidence. |
| [12 · Standards and references](/architecture/12-references) | Protocol standards, open implementations and integration references for the GRAMStack architecture. |

[Read the complete design](/architecture/complete-design) · [Download Markdown](/static/gramstack-design.txt)

For the running code and operational details, continue to [Implementation](/implementation). For integration contracts, use [Developer resources](/developers).
