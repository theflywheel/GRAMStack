---
title: Evidence, state and persistence
description: Versioned GRAMStack implementation contracts and conformance requirements.
---

Specification version **0.1.0** · [Specifications index](/specifications/) · [Download source](https://docs.gramstack.try-dough.com/static/specs/0.1.0/evidence-and-state.md)

# Evidence, state and persistence

## Durable boundaries

| Owner | Durable state | Ephemeral state |
|---|---|---|
| Gateway | Bounded lifecycle checkpoints, request digest, idempotency and event deduplication metadata | Redis TTL interaction/session and routing context |
| Consent authority / AuthZ enforcement | SQL notices, decisions, signed artefacts, status versions, outbox, one-use reservations and replay records | Cache only; never authoritative for withdrawal/redemption |
| Adapter | Attempt identity, redemption reference, source-read outcome and signed evidence reference | In-memory projected business response during one delivery |
| Evidence custodian | Immutable signed receipts and durable anchoring outbox | Retry scheduling |
| Source registry | Authoritative business records | Registry-controlled lookup/cache |

Do not persist source business data, raw identity claims or plaintext responses in gateway logs, replay stores, queues or evidence. Operational retention policies set TTLs and SQL cleanup/backups. Expiring Redis does not delete durable consent or evidence.

## Evidence before release

The adapter signs an EvidenceEvent as compact JWS, ES256, typ=gramstack-evidence+jwt with its approved kid. Evidence commits to request, consent, grant jti, adapter, attempt/redemption IDs, fields, decision/outcome and a protected response commitment. The commitment is SHA-256 over a cryptographically random 32-byte salt concatenated with UTF-8 RFC 8785 canonical JSON of the projected resource data. Custody of the salt is access-controlled; do not expose it in public anchoring data. A release_ready event requires the response commitment. Failure events omit it when no projected response exists. Never publish plaintext or guessable unsalted hashes of personal values.

The custodian authenticates the producer, verifies its signature and scope, writes immutable receipt bytes and the anchoring outbox atomically, then acknowledges receipt_id, evidence_digest, accepted_at and anchor_status. The evidence digest hashes the exact compact-JWS UTF-8 bytes. Duplicate event IDs with identical bytes return the same acknowledgement; conflicting bytes fail. No protected payload is released before this durable acknowledgement.

A deployment declares synchronous anchoring or durable batch anchoring. Synchronous profile waits for `anchored`; batch profile can release at `pending` only after receipt and outbox durability. Failure to persist evidence prevents release. DeDi stores protected commitments and anchor references; it does not hold business data. Receipts establish accountable processing and release attempts, not proof of client receipt or plaintext deletion.

## Lifecycle

`lifecycle.json` is the allowed transition map. Mutations use versioned compare-and-swap and durable checkpoints; duplicate events cannot move state backward. Gateway owns lifecycle processing as a backend module. Human interaction and event delivery are asynchronous; policy checks, atomic redemption and evidence acknowledgement are synchronous release gates.

COMPLETED requires the server to finish its response write, which still cannot prove the client retained the data. DELIVERY_UNKNOWN captures a failure or crash after a release might have begun. Recovery after a reserved grant never automatically fetches or sends data again. A crash before reservation can resume validation; recovery after evidence durability can report the receipt without replaying data. Terminal state is immutable, with reconciliation recorded as additional evidence rather than rewriting history.

Consent events, grant redemption and source retrieval are correlated using request_id and request_digest. Receipt access is owner-authorized and contains no source plaintext. Browser receipt returns safe request/consent status and authorized receipt references, not the raw signing secrets or protected source mapping.

[Repository source](https://github.com/theflywheel/GRAMStack/tree/main/specs/0.1.0/evidence-and-state.md).
