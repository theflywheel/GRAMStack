---
title: 02 · High-level architecture
description: Logical components, runtime boundaries, control plane and protected data path.
date: 2026-11-04
---
# High-level architecture

## Interactive architecture walkthrough

Follow a SakhiCredit request from sign-in to the access audit. Select a step to see the components, call, and request state behind it.

<iframe src="/static/architecture-walkthrough.html" title="Interactive GRAMStack architecture walkthrough" width="100%" height="750" loading="lazy"></iframe>

[Open the walkthrough in a full window](static/architecture-walkthrough.html)

## Component and trust-boundary view

```mermaid
graph TB
    C["Consumer backend"] --> API
    U["Subject browser"] --> LOGIN
    U --> UI
    subgraph CORE["GRAMStack reference deployment"]
        subgraph GW["Gateway application"]
            API["API perimeter and routing"]
            LIFE["Request lifecycle manager"]
            LOGIN["AuthN handlers and IdP adapters"]
            UI["Consent UI"]
            CM["Consent module"]
            AZ["AuthZ and grant issuer"]
        end
        H["Hydra protocol engine"]
        HP[("Hydra SQL")]
        W[("Shared TTL workflow store")]
        CS[("Consent and enforcement SQL")]
        CFG["Versioned trust and registry policies"]
        KEY["Secure signing keys"]
    end
    IDP["External OIDC or SAML IdPs"]
    VERIFY["Registry-approved subject verification"]
    subgraph REG["Registry-controlled boundary"]
        AD["LokOS adapter"]
        SRC["Existing LokOS APIs and records"]
    end
    EV[("Protected evidence custody")]
    DE["DeDi commitments and proofs"]
    API --> LIFE
    LIFE --> W
    LIFE --> CM
    LIFE --> AZ
    LOGIN --> H
    H --> HP
    LOGIN --> IDP
    LIFE -.-> VERIFY
    VERIFY -.-> AD
    UI --> CM
    CM --> CS
    AZ --> CS
    AZ --> CFG
    AZ --> KEY
    API --> AD
    AD --> AZ
    AD --> SRC
    CM --> EV
    AZ --> EV
    AD --> EV
    EV --> DE
```

Sharing a deployment does not merge trust decisions. An externally exposed gateway must not expose Hydra admin APIs, private redemption APIs or signing keys.

## Responsibilities and contracts

| Component | Inputs | Outputs | Authoritative responsibility |
|---|---|---|---|
| API perimeter | Consumer token, request payload | Validated caller context, safe errors | Consumer authentication, quotas and routing |
| Lifecycle manager | API commands and verified callbacks | State transitions, deadlines, references | Overall request progress |
| AuthN integration | OIDC/SAML transactions | Issuer-qualified verified principal | Authentication context, not registry ownership |
| Subject verification | Principal and immutable request | Registry-approved request-bound context | Person-to-record relationship; registry-specific contract |
| Consent module | Verified context, request, human decision | Signed artefact and current status | Human disclosure approval and withdrawal |
| AuthZ | Consumer, consent, context, policy | Decision, scoped grant and redemption result | Permission to execute a particular read |
| Registry adapter | Authenticated caller and grant | Canonical approved fields and signed receipt | Independent source-side enforcement |
| Evidence subsystem | Signed events and artefacts | Durable acknowledgement, proof references | Accountable custody and anchoring |
| Discovery | Signed configuration | Capabilities, trusted issuers and endpoints | Configuration distribution, not citizen search |

## Consent control plane versus data plane

```mermaid
graph LR
    C["Consumer"] --> G["Gateway"]
    G --> CM["Consent control plane"]
    CM --> Z["AuthZ"]
    Z --> G
    G --> A["Adapter"]
    A --> S["Source"]
    S --> A
    A --> G
    G --> C
    A -. "Signed metadata only" .-> E["Evidence custody and DeDi"]
```

The consent module sees requested data categories and necessary identity references, not the returned membership/savings response. The gateway and adapter may process protected response bytes transiently. Therefore the overall gateway is not described as data-blind merely because the consent module is.

## Deployment guidance

The reference deployment uses one GRAMStack application containing gateway, lifecycle, login and consent handlers and AuthZ modules. Hydra and stores remain separate processes. Registry adapters can be embedded by registry owners or deployed in a controlled environment using exclusive integration credentials. Isolation of signing and consent services can be introduced when concrete operational or security requirements justify it.

The component contracts are language-independent. Deployment boundaries follow credential isolation, availability and scaling requirements rather than requiring a separate service for every logical module.

## Synchronous and asynchronous boundaries

| Boundary | Model | Consequence |
|---|---|---|
| Consumer authentication | Synchronous token exchange | No registry-data authority implied |
| Create access request | Synchronous validation, then `202` | Human approval is not held open in an HTTP request |
| External login | Browser redirects and callbacks | Persist state/nonce/PKCE correlation across instances |
| Registry verification | Immediate result or asynchronous challenge | Never assume an HTTP timeout is a rejection |
| Human decision | Asynchronous interaction | Subject can reject, abandon or time out |
| Artefact signing / evidence | Durable background work if required | Approval can be recorded before usable consent exists |
| Status propagation | At-least-once notifications plus reconciliation | Duplicate and missed deliveries are expected |
| Grant issuance | Bounded synchronous decision | Explicit consumer call |
| Redemption and retrieval | Bounded synchronous execution | Atomic reservation before source access |
| DeDi anchoring | Synchronous or batched according to deployment policy | Release depends on the chosen durable evidence boundary |

## Exclusive integration claim

Every GRAMStack-facing retrieval must pass authorized enforcement and produce required evidence before protected release. Upstream credentials must not be exposed to consumers. This claim covers GRAMStack-mediated retrieval, not unrelated native LokOS applications, administrative access or other integration credentials.


## Adapter discovery and onboarding trust

```mermaid
graph TD
    O["Operator-approved descriptor: origins, identity, resources, method and public keys"] --> V["Gateway validates public discovery metadata"]
    M["Adapter public manifest and JWK set"] --> V
    V --> D{"Exact approved contract and key material?"}
    D -->|No or unavailable| N["Block before dispatch: no attempt or source call"]
    D -->|Yes| T["Original sender, grant and request checks"]
    T --> A["Private service-mTLS adapter retrieval"]
    A --> R["Atomic redemption and evidence-before-release"]
```

An independently hosted adapter publishes public capabilities and public verification keys. Its protected verification/retrieval RPCs use a separate mTLS listener. The gateway's descriptor pins the public metadata origin, private data origin/service identity, registry, adapter ID/audience, verification method, resource name/version/fields and approved signing key material. Discovery confirms the deployed contract; it cannot enroll a key, choose a new host, broaden policy or verify a person's registry ownership.

The [adapter discovery profile](specifications/adapter-discovery) defines fixed well-known paths, explicit versions, bounded JSON and strict EC/P-256 public JWKs. Before each protected dispatch, gateway checks the advertised method, required capabilities, resource versions and exact approved key points. Redirects, private key parameters, unknown fields, duplicate identities and unsupported algorithms fail closed. Metadata reads send no consumer token, cookie or private service credential. Production metadata uses HTTPS; explicit development permits literal-loopback HTTP only.

Unavailable or incompatible metadata returns a safe failure before the gateway claims an attempt. The request can proceed after valid metadata is restored. Actual permission still requires current source policy, verified subject, ACTIVE consent, original consumer certificate, independent SDK checks, atomic one-use admission and durable evidence. The manifest is a capability declaration, with dependency availability checked separately from trust.


## SakhiCredit consumer product boundary

SakhiCredit is an independent consumer, with its own Go module, browser experience and database. Its backend uses both GRAMStack interfaces: the OAuth-authenticated control API for request/status/grant operations, and the private consumer-mTLS data API for one admitted disclosure. The product browser uses a separate product session; it receives neither consumer credentials nor retrieval grants. Authentication, source ownership, exact notice, consent and access enforcement stay in the platform.

```mermaid
sequenceDiagram
    participant B as SakhiCredit browser
    participant P as SakhiCredit backend
    participant S as Product SQL
    participant G as GRAMStack gateway
    B->>P: POST application with browser session and CSRF
    P->>S: Save immutable encrypted application input
    P->>G: POST access request using consumer OAuth
    G-->>P: Request ID and first-party interaction URL
    P->>S: Bind the platform request to the application
    P-->>B: Saved progress and sharing handoff
    B->>G: Sign in, verify source record and decide in a separate tab
    Note over G: Consent activation and lifecycle<br/>projection use durable workers
    B->>P: Check application progress
    P->>G: GET request status using consumer OAuth
    G-->>P: Current workflow status
    P-->>B: Permission ready or a stopped outcome
    B->>P: Explicitly request approved details
    P->>G: POST grant with stable idempotency key
    G-->>P: Scoped short-lived retrieval grant
    P->>S: Commit one fixed-deadline fetch claim
    P->>G: GET data over consumer mTLS with the grant
    Note over G: SDK admission and exact projection<br/>with durable evidence before release
    G-->>P: Approved fields and access receipt reference
    P->>S: Commit encrypted received result
    P-->>B: Present the approved fields
    Note over B,S: Reload reads the existing product copy. It does not fetch again
```

The product presents membership status and an optional source-reported INR savings summary. A person chooses the requested fields before creation, then approves or rejects the whole exact notice in the first-party portal. Changing scope requires cancellation and a new application. The reference product prepares information for credit review; it does not decide eligibility or issue a loan.

| Boundary | Execution and persistence | What the person sees |
|---|---|---|
| Application creation | Product SQL commits before the platform call; stable idempotency recovers a lost response | Saved application and identity/consent handoff |
| Identity, ownership and notice | Platform-controlled interaction in a separate tab; product credentials remain server-side | Exact request, verification and explicit approve/reject controls |
| Progress | Browser polls its backend; backend checks platform metadata, without reading source data | Waiting, ready, rejected, withdrawn, cancelled or expired |
| Approved retrieval | One product fetch claim, then the platform's independent one-use admission/evidence gate | In-flight state followed by approved details or a safe uncertain outcome |
| Reload/restart | Product reads its encrypted copy; ambiguous or abandoned attempts never regain a source-fetch slot | Existing received result or a clearly reported uncertain outcome |
| External evidence | Batch custody is synchronous with release; DeDi publication is asynchronous | A durable sharing reference, without claiming external publication has completed |

The reference browser experience is English, responsive to desktop/mobile widths, and uses only locally served assets. It retains only an opaque application ID in session storage. The synthetic demo uses browser-session possession for product ownership and fictional source/identity fixtures. Production customer accounts, onboarding and a lending decision policy remain separate product responsibilities; the platform's authenticated person-to-source verification is not replaced by this demo session.
