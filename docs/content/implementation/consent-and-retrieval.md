---
title: Consent and retrieval processing
---

The gateway owns request coordination. `looplab/fsm` defines allowed transitions; SQL transactions and expected-version checks make those transitions durable and safe across replicas.

| Operation | Synchronous boundary | Work that follows |
|---|---|---|
| Create request | Validate consumer, scope and terms; commit immutable snapshot, idempotency binding and outbox | Person completes browser interaction |
| Verify registry ownership | Call the approved adapter verifier and retain bounded subject context | Present exact notice |
| Record approval | Persist the session-bound decision and notice evidence | Worker signs and activates consent with recovery across restarts |
| Issue grant | Recheck policy and active consent; issue a short-lived sender-bound grant | Consumer initiates retrieval |
| Retrieve | Admit once, dispatch over mTLS, read exact source fields and durably accept signed evidence before release | DeDi anchoring proceeds asynchronously |

The Consent Manager handles notice, decision, activation and withdrawal. Withdrawal and retrieval admission lock the same consent authority so their ordering is explicit. Scope changes require cancellation and a fresh request.

The adapter uses the Go SDK for grant checks, reservation, exact projection and evidence submission. Ambiguous delivery does not reopen authority. SakhiCredit persists its retrieval attempt before making the SDK call and retains an encrypted authorized copy for the demo's bounded retention period.

[Lifecycle and recovery](/architecture/06-lifecycle) · [Release boundaries](/architecture/08-state-and-evidence#the-release-boundary) · [Consent Manager source](https://github.com/theflywheel/GRAMStack/tree/main/services/gateway/internal/consentmanager)
