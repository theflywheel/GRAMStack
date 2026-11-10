---
title: Overview
description: Architecture, specifications, and SDKs for consent-first data sharing in public infrastructure.
---

<div class="hero-kicker">GRAMStack documentation</div>
<div class="hero-title">One request.<br>Explicit consent.<br>Verifiable access.</div>
<p class="hero-summary">Connect applications to authoritative registries through federated authentication, verified ownership, explicit consent and scoped retrieval.</p>

<div class="docs-paths">
<a class="docs-path" href="/architecture"><span class="path-label">Understand</span><strong>Explore the architecture ↗</strong><span>Follow the 12 chapters, diagrams and end-to-end dry runs.</span></a>
<a class="docs-path" href="/developers"><span class="path-label">Build</span><strong>Build with GRAMStack ↗</strong><span>Use the SDKs, implement an adapter, or inspect the API contracts.</span></a>
<a class="docs-path" href="/demos"><span class="path-label">Try</span><strong>See the platform working ↗</strong><span>Try SakhiCredit, watch the recording and check the demo boundaries.</span></a>
</div>

## Find your starting point

| You want to… | Start here |
|---|---|
| Understand the design and trust model | [Architecture](/architecture) |
| Work on this repository or operate the deployment | [Implementation](/implementation) |
| Integrate an application | [Integration quickstart](/developers/quickstart) |
| Build a registry adapter | [Building adapters](/developers/adapters) |
| Inspect accepted messages and versioned contracts | [Specifications](/developers/specifications) |
| Review the live product and its evidence trail | [SakhiCredit demo](/demos/sakhicredit) |

## The trust path

Authentication establishes an identity. Registry verification establishes the relationship to a source record. Consent approves an exact disclosure. Authorization checks policy and scope. Retrieval produces access evidence.

Each step is enforced separately. Application sign-in does not grant data access, and a valid token alone does not prove that a disclosure occurred.

[Complete architecture document](/architecture/complete-design) · [Source repository](https://github.com/theflywheel/GRAMStack)
