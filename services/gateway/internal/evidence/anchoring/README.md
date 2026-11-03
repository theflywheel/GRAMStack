# Durable batch anchoring

Enable `authorization.evidence.anchoring_enabled=true` with `release_profile=batch`. The gateway validates pinned DeDi configuration before starting, runs one worker and cancels/joins it before closing the SQL pool. Startup never provisions a remote namespace implicitly. Apply migration 0011 explicitly; it makes anchored projection/proof updates immutable.

The worker uses subscriber `evidence.anchoring.v1`, bounded outbox seeding, 30-second fenced leases, 20-second operations and persistent exponential retry (1–64 seconds). It rereads immutable signed custody and historical admission rather than trusting an event payload. A failed verification or publication stays pending. Retired adapter/grant verification keys must remain available for retained history.

DeDi receives only `gramstack.evidence.v1` and the SHA-256 digest of compact signed evidence. The opaque custody receipt ID derives the immutable record name. Create-only publication and exact proof verification reconcile ambiguous network/SQL failures. The verified `pending → anchored` FSM projection and one metadata `evidence.anchored` outbox event commit together. A lost delivery acknowledgement can verify the saved inclusion proof without contacting DeDi. This worker never reads source data or changes authorization.

## Operator configuration

Use the same environment as `gramstack anchor`:

- `DEDI_NODE_URL`, `DEDI_NAMESPACE`, `DEDI_REGISTRY`
- `DEDI_PUBLISHER_KEY_ID`, `DEDI_PUBLISHER_KEY_FILE` (base64 Ed25519 private key, restricted file)
- `DEDI_ADMIN_USER`, `DEDI_ADMIN_PASSWORD`
- `DEDI_CHECKPOINT_ORIGIN`, `DEDI_CHECKPOINT_VERIFIER` (explicit pinned note verifier)

HTTPS is mandatory except explicit development configuration permitting literal loopback HTTP. Provision the namespace/registry through the explicit operator client/CLI before publication. Keep credentials out of logs and public configuration. Each node request is bounded to ten seconds with no redirects. Receipt acknowledgement remains the original durable/pending acknowledgement, while `evidence_anchor_state` retains current status and full inclusion reference separately. Protected owner history/API exposure is a separate delivery surface.

Batch mode permits release after SQL custody even while DeDi is unavailable. Monitor pending age/backlog and establish an operational outage policy; retry does not enforce a maximum pending duration. Synchronous custody release is not implemented. Inclusion proves membership under a pinned signed checkpoint, not independent witnessing, cross-checkpoint consistency, recipient delivery or deletion.

## Acceptance

`PG_BIN=... DEDI_SOURCE=/path/to/DeDi-node ./scripts/test-dedi.sh` runs actual-node publication and PostgreSQL tests: lost acknowledgement, unavailable node and persistent backoff, concurrent publication, publication followed by SQL rollback, first-version reconciliation, proof retention and monotonic SQL projection. No fake proof replaces the real node. The existing hosted DeDi job runs these tests without an extra job or container pull.

For full configured gateway/SDK/native-source/process-restart acceptance, add `GRAMSTACK_TEST_ANCHORING=1 HYDRA_BIN=/path/to/hydra`; add `GRAMSTACK_TEST_ESIGNET=1` when the isolated official eSignet stack is running. The full process check waits for every real custody receipt to be anchored and independently verifies its retained proof offline before and after restart. The standard Hydra CI job retains all prior batch/pending data-path coverage; actual-node worker recovery runs in the DeDi CI job.
