---
title: Persistence, evidence and DeDi
---

The Railway demo uses one PostgreSQL server with separate databases and dedicated roles for service ownership. Sharing the server does not turn the databases into a shared application schema.

| Owner | Stored information |
|---|---|
| Gateway | Immutable requests, protected locator snapshots, consent records, one-use enforcement, metadata outbox and evidence custody |
| Adapter | Encrypted verified subject mappings and durable retrieval-attempt state |
| SakhiCredit | Browser-bound login state, identity sessions, product progress and encrypted received results |
| Hydra and eSignet | Their service-owned protocol and identity-provider state |
| DeDi | Its node-owned publication and verification state |

Gateway and adapter do not retain a general-purpose copy of source business records. The consumer product owns its approved received copy. Sensitive mappings and product payloads have separate custody and expiry boundaries.

Approval use is recorded when the adapter reserves the authorized disclosure. Signed evidence and response outcome are tracked separately. The first-party consent-history UI exposes the durable audit trail; a receipt can initially show anchoring pending while its DeDi work item is processed.

DeDi anchors commitments to evidence. It does not establish that a source assertion is true, erase a consumer's plaintext copy or prove the absence of access outside GRAMStack.

[State inventory and evidence model](/architecture/08-state-and-evidence) · [Live audit walkthrough](/demos/sakhicredit/walkthrough#try-the-access-audit) · [DeDi integration source](https://github.com/theflywheel/GRAMStack/tree/main/services/gateway/internal/evidence/dedi)
