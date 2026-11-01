---
title: 12 · Standards and references
description: Protocol standards, open implementations and integration references for the GRAMStack architecture.
date: 2026-11-01
---
# Standards and references

GRAMStack combines federated authentication, consent-based sharing, scoped capabilities and tamper-evident access evidence. The following standards and implementations inform those boundaries.

## DEPA and concrete consent references

| Source | Use |
|---|---|
| [iSPIRT DEPA architecture](https://github.com/iSPIRT/DEPA) | Architectural background and older draft API; not a runtime dependency |
| [DEPA World](https://depa.world/) | Conceptual flows and documentation; verify current ownership/version before normative use |
| [NITI Aayog DEPA discussion paper](https://niti.gov.in/sites/default/files/2023-02/DEPA-Book.pdf) | Separation of consent/data flows and data-blind consent role |
| [ReBIT API portal](https://api.rebit.org.in/) | Governed financial consent/data-sharing specifications; domain assumptions must be separated |
| [ReBIT AA API](https://api.rebit.org.in/spec/aa) | Consent lifecycle and artefact reference |
| [Sahamati standards](https://github.com/Sahamati/account-aggregator-standards) | AA schemas/contracts |
| [Sahamati conformance](https://github.com/Sahamati/certification-framework) | Ecosystem test structure |
| [DPDP Act — India Code](https://www.indiacode.nic.in/bitstream/123456789/22037/2/a2023-22.pdf) | Legal role definition; deployment software is not operator registration |

DEPA supplies the architectural reference for consented sharing. ReBIT maintains domain-specific financial-data-sharing standards. iSPIRT's [training](https://github.com/iSPIRT/depa-training) and [inferencing](https://github.com/iSPIRT/depa-inferencing) projects address additional privacy-preserving sharing/processing scenarios; they are not drop-in consent-manager successors.

## Open implementation reading list

| Implementation | Links | Licensing/assessment |
|---|---|---|
| GovStack / iGrant Consent BB | [Go API](https://github.com/decentralised-dataexchange/bb-consent-api), [sharing UI](https://github.com/decentralised-dataexchange/bb-consent-data-sharing-ui), [privacy dashboard](https://github.com/decentralised-dataexchange/bb-consent-privacy-dashboard), [playground](https://github.com/decentralised-dataexchange/bb-consent-playground) | Apache-2.0; generic consent APIs, agreement revisions and user interfaces |
| GovStack specification/test working group | [Specification and examples](https://github.com/GovStackWorkingGroup/bb-consent) | Consent building-block specification and examples |
| TSI DPDP CMS | [Implementation and guides](https://github.com/tsi-coop/tsi-dpdp-cms) | Apache-2.0; India-focused notice, consent and withdrawal patterns |
| ProjectEKA | [Health consent manager](https://github.com/ProjectEKA/consent-manager) | Source visible; licensing must be confirmed before code reuse |

Inspect identity creation, status consistency, signing/key handling, agreement revisions, notice provenance, withdrawal, webhooks/reconciliation, security maintenance, dependencies, tests and evidence export. Published feature lists are not proof of production suitability.

## Authentication references

- [Ory Hydra](https://github.com/ory/hydra) and [Ory documentation](https://www.ory.com/docs/hydra)
- [Historical Hydra token-exchange discussion](https://github.com/ory/hydra/discussions/3359): exact current release must be checked rather than assuming a historical limitation persists.
- [MOSIP eSignet](https://github.com/mosip/esignet) and [mock services](https://github.com/mosip/esignet-mock-services)
- [MeriPehchaan onboarding](https://meripehchaan.gov.in/applicationonboarding) and [JanParichay partner portal](https://jppartners.meripehchaan.gov.in/)
- [OpenID Connect Core](https://openid.net/specs/openid-connect-core-1_0.html)
- [OAuth security best current practice, RFC 9700](https://www.rfc-editor.org/rfc/rfc9700)
- [Token exchange, RFC 8693](https://www.rfc-editor.org/rfc/rfc8693), [resource indicators, RFC 8707](https://www.rfc-editor.org/rfc/rfc8707), [rich authorization requests, RFC 9396](https://www.rfc-editor.org/rfc/rfc9396)
- [DPoP, RFC 9449](https://www.rfc-editor.org/rfc/rfc9449), [OAuth mTLS, RFC 8705](https://www.rfc-editor.org/rfc/rfc8705)
- [JSON canonicalization, RFC 8785](https://www.rfc-editor.org/rfc/rfc8785), as a candidate canonicalization profile to evaluate.

## Integration and assurance requirements

Deployments verify the exact Hydra release's grant types, sender-constraint support, hooks and retention behavior. Inspect real SQL rows, janitor cleanup, logs and backups; opaque subjects do not imply anonymous storage. Confirm eSignet adapter APIs and principal semantics from code. Obtain actual government partner docs, source-system contracts and onboarding access instead of treating old sandbox statements as guarantees.

The operating model defines the applicable consent profile, legal operator roles, commencement/retention obligations, evidence custody and audit process against current authoritative law and policy. An illustrative signed payload is not a certification or registration claim.

## Source repository

The architecture documents, Quartz site, build scripts and Railway deployment configuration are maintained in [theflywheel/GRAMStack](https://github.com/theflywheel/GRAMStack). The site source lives under `docs/`, with chapter Markdown under `docs/content/`.

## This site

Built with [Jacky Zhao's Quartz](https://github.com/jackyzha0/quartz), with Markdown sources, search, chapter links, Mermaid diagrams, a complete reading page and Markdown export. Diagrams are technical design illustrations; fallback step tables explain the same critical flow when scripts are unavailable. Railway hosts static output through a small HTTP server; it hosts no GRAMStack application, identities or registry data.

## Versioned implementation contracts

Use the [implementation specifications](specifications/) for the normative API, message and adapter contracts. [OpenAPI](specifications/openapi), [message schemas](specifications/schemas), [adapter requirements](specifications/adapter) and [conformance scenarios](specifications/conformance) are published from the repository’s canonical `specs/` sources.
