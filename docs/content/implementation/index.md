---
title: Implementation
---

How the architecture is realised in the Go services, durable state and Railway deployment. These guides describe the running implementation; versioned wire requirements live in [Specifications](/developers/specifications).

| Guide | What you will find |
|---|---|
| [Components and repository layout](/implementation/components) | Modules, packages, products, adapters and mocks |
| [Authentication: Hydra and eSignet](/implementation/authentication) | Consumer OAuth, application sign-in and request-bound identity |
| [Consent and retrieval processing](/implementation/consent-and-retrieval) | FSM transitions, sync/async work and the release boundary |
| [Persistence, evidence and DeDi](/implementation/persistence-and-evidence) | Database ownership, evidence custody and anchoring |
| [Deployment and configuration](/implementation/deployment) | Service builds, private endpoints, secrets and migrations |
| [Testing and operational verification](/implementation/verification) | Local checks, integration suites and evidence validation |
