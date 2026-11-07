---
title: Capabilities and limitations
---

## Working

- Standalone GRAMStack application sign-in, real Hydra OAuth/OIDC and eSignet federation.
- Registry ownership verification, exact notice, explicit approval/refusal, cancellation and withdrawal.
- Certificate-bound one-use retrieval, field projection, durable evidence custody and asynchronous DeDi anchoring.
- Persistent consent history and access audit, protected receipt export and rights-case intake.
- SDK-backed SakhiCredit with encrypted received copies, bounded retention and authenticated session rotation.

## Simulated inputs

The identities, one-time codes, LokOS-facing source records and credit-review scenario are fictional. eSignet uses a mock identity backend. Protocol implementations run, but the demo is not connected to a government identity or registry production system.

## Unsupported or outside this demo

- Government production credentials, certification or a claim of regulatory compliance.
- Real lending decisions, loan offers, source-record writes or a general cross-registry identity graph.
- Silent partial approval: a different scope requires cancellation and a new explicit request.
- Proof that a consumer deleted plaintext, or proof that no access occurred outside GRAMStack.
- Other product datasets without approved resource profiles and registered adapters.

[Detailed failure and security boundaries](/architecture/10-failures) · [Try the demo](/demos/sakhicredit/try-it)
