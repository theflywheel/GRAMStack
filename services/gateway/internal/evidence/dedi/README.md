# DeDi-node integration

This client implements the publication/proof extension of [theflywheel/DeDi-node](https://github.com/theflywheel/DeDi-node), inspected at revision `dbdb9f6ce8f69b8253f405f9c14ea345cd9e6d1f`.

- Writes use namespace-scoped Ed25519 publisher signatures over the method, URI, body digest, timestamp and create-only precondition, plus the configured admin transport credential.
- Only an opaque event-derived record name, schema version and protected evidence digest leave the gateway. No consent payload, source data or raw identity is sent.
- A retry receives 412 and reconciles against the existing record; conflicting commitments fail instead of overwriting.
- Publication succeeds only after checking the exact payload, publisher attribution, immutable first version, trusted checkpoint signature/origin and Merkle inclusion proof using Go's sumdb note/tlog packages.
- HTTPS is required except an explicitly enabled literal loopback address for isolated development tests. Redirects, unbounded responses, missing proofs and untrusted keys fail closed.

Inclusion establishes membership under the pinned node's checkpoint. The [durable worker](../anchoring/README.md) retains the full proof for offline verification and restart recovery. Cross-checkpoint consistency and independent witness verification are not implemented; this client alone does not establish non-equivocation or client receipt of data.

`TestLiveNode` runs against an isolated actual node when GRAMSTACK_TEST_DEDI_* variables are configured. It never uses production nodes or credentials by default. The integration runner under `scripts/` provisions a temporary local database and node from an explicitly supplied checkout.
