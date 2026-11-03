# One-dispatch protected data delivery

The gateway's private TLS listener serves core 0.3.0 GET `/v1/access-requests/{id}/data` when `authorization.retrieval` is explicitly configured. The caller supplies the issued retrieval grant and the registered consumer client certificate. Gateway checks actual verified TLS leaf bytes against `cnf.x5t#S256` and registration; forwarded identity/certificate headers cannot authenticate a sender. A generic public TLS-terminating proxy cannot replace that proof. Public request/status/portal traffic remains on its existing listener.

The gateway loads its approved adapter origins/server names from the existing registry descriptors, joins them to configured adapter IDs/audiences and rejects identity disagreements. It imports no registry implementation or SDK source mapping. Enable with `authorization.retrieval: {"max_concurrent":4}` alongside configured evidence custody. Adapter credentials and runtime settings remain adapter-local. All endpoint URLs are operator-controlled HTTPS origins; redirects and user-supplied destinations are rejected.

## Admission and response gates

1. Verify grant, original sender, path and immutable request/snapshot binding. HEAD, query/body variants and wrong sender fail before dispatch.
2. In one SQL transaction, claim a unique request/attempt and metadata outbox event. The fixed DB-clock deadline is 60 seconds. No network call holds this transaction open.
3. Send one service-mTLS POST to the separately hosted adapter with snapshot, grant and trusted presenter context. It independently validates and redeems through AuthZ before its source read.
4. Accept only the expected contract, no-store JSON, request ID, exact selected field set and bounded response. Verify retained signed custody against grant, adapter and attempt before writing any outward resource bytes.
5. Atomically persist request/dispatch release intent, receipt reference and FSM/outbox. Write the resource once, then record COMPLETED or DELIVERY_UNKNOWN. Completion describes the server write, not proof of client retention.

`looplab/fsm` decides request and dispatch transitions; SQL locks/version checks decide persistence. Duplicate calls return 409 and never replay cached business data. A rolled-back dispatch is retryable because no adapter was called. After a committed dispatch, transport ambiguity cannot cause a resend. Authoritative use remains spent after source/evidence failure. The gateway stores grant digests, presenter/attempt/receipt references and outcomes, never the source response or raw grant in the dispatch table.

## Recovery

An embedded bounded loop checks at most 32 expired running dispatches per pass, with request-then-dispatch lock ordering and DB-time rechecks. Before admission it records FAILED; after admission or a possible write it records DELIVERY_UNKNOWN. It never calls the adapter or source. Deadline/scope fields are immutable and a stale worker cannot revive the dispatch. Release checkpoints recheck deadline and workflow under lock. A lost final checkpoint leaves RELEASE_ATTEMPTED occupied until recovery. Stop cancels/joins the loop before closing the pool; private HTTP shutdown bounds in-flight work.

## Acceptance

Real SQL tests exercise response/receipt field gates, partial writes, dispatch/release/final-checkpoint rollback, immutable deadlines and abandoned attempts before/after admission. `GRAMSTACK_TEST_RETRIEVAL=1 PG_BIN=... HYDRA_BIN=... ./scripts/test-hydra.sh` includes all previous gates and actual consumer mTLS → gateway → SDK → mock native read → real custody → response, concurrent/native-transport replay denial, outage/disconnect/custody failures, exact one/two-field projection, withdrawal on both sides of the cutoff and three-process restart. Approval fixtures are queued together before waiting, preserving production one-second worker polling while reducing repeated waits. Add `GRAMSTACK_TEST_ESIGNET=1` for official eSignet and its mock identity backend. This is functional protected disclosure, not a product UI, SDK publication or verified automatic DeDi anchoring claim.
