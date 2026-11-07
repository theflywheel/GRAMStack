---
title: Components and repository layout
---

The repository contains independent Go modules for the gateway, application SDK, adapter SDK, registry adapters and SakhiCredit. The root `go.work` supports development across those modules; there is no root Go module.

| Repository path | Responsibility |
|---|---|
| `services/gateway/` | Request coordination, AuthN integration, Consent Manager, authorization, enforcement and evidence custody |
| `sdk/go/client/`, `sdk/python/`, `sdk/javascript/` | First-party application integration |
| `sdk/go/adapter/` | Independently publishable adapter SDK |
| `adapters/lokos/` | Reference registry adapter built with the Go adapter SDK |
| `adapters/auth/oidc/` | Provider adapter for federated OIDC authentication |
| `apps/sakhicredit/` | SDK-backed consumer product, authenticated sessions and encrypted received copies |
| `mocks/` | Separately runnable fictional identity and source systems |
| `infra/` | Infrastructure definitions and deployment tooling |
| `specs/` | Canonical, versioned contracts |
| `docs/` | Quartz application, Markdown and generated reference pages |

The Consent Manager is an explicit gateway subsystem. Registry-specific lookup, transformation and native credentials belong to the independently hosted adapter. Product decisions and received-data storage belong to the consumer application.

[Component and trust-boundary diagram](/architecture/02-architecture#component-and-trust-boundary-view) · [Repository source](https://github.com/theflywheel/GRAMStack)
