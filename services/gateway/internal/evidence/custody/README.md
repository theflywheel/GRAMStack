# Durable evidence custody

Custody accepts core 0.3.0 ES256 `gramstack-evidence+jwt` envelopes from registered adapter mTLS identities. The configured adapter ID, audience, service DNS identity and public signing keys are separate trust inputs. Token-supplied key URLs and unknown claims/headers cannot add trust. The gateway does not need the adapter's private signing key, source response or private commitment salt.

`authorization.EvidenceScope` verifies the immutable stored grant and its persisted reservation, including request/digest, consent, grant JTI/digest, presenter, adapter audience and admission time within the signed grant bounds. Custody matches the event's exact fields and all admission references. The event timestamp is reported by the producer; it must be within the allowed 30-second skew around admission/current DB time. Acknowledgement time comes from SQL. Evidence remains recordable after expiry, withdrawal or policy revocation because it describes an admitted attempt. This neither reopens the request nor authorizes another fetch.

A receipt transaction serializes the event ID, locks the request and writes exact compact-JWS bytes, immutable receipt metadata, pending anchor state, a metadata-only `evidence.received` outbox event and the applicable request FSM transition atomically. A release-ready event advances REDEEMING to EVIDENCE_COMMITTED. The same signed bytes recover the same receipt ID/digest/acceptance time after a lost acknowledgement or restart, including after terminal completion. Conflicting bytes for an event ID fail. Only one release-ready receipt exists per reservation. Failure/uncertain reports never move a terminal lifecycle backward.

The explicit implemented custody profile is `batch`: HTTP 201 means the receipt and anchoring work committed durably, with `anchor_status=pending`. It does not mean DeDi publication, client receipt or deletion proof. The optional [anchoring worker](../anchoring/README.md) publishes and verifies the committed work asynchronously; selecting synchronous release without an anchored acknowledgement must withhold the response.

## Configuration

Enable the private endpoint through optional authorization configuration:

```json
{
  "evidence": {"release_profile":"batch","anchoring_enabled":true},
  "adapters": [{
    "registry":"lokos",
    "adapter_id":"lokos-adapter",
    "audience":"lokos-adapter",
    "service_identity":"lokos-adapter.gramstack.internal",
    "evidence_keys":[{"key_id":"adapter-key-v1","file":"/run/keys/adapter-evidence.pub"}]
  }]
}
```

This extends the existing authorization configuration. The endpoint is POST `/internal/v1/evidence` on its private TLS listener, requires service mTLS, `Content-Type: application/json` and `Gramstack-Contract-Version: 0.3.0`, and accepts only `{"jws":"<compact signed event>"}`. It is absent from the public HTTP listener. Keep retired verification public keys available for retained signed history; private keys stay with the producer. Migration 0009 is explicit, checksummed and never applied by startup.

The `evidence_receipts` table retains signed metadata only and rejects updates. `evidence_anchor_state` holds later anchoring projection separately. The outbox carries receipt/evidence digests and opaque references; it carries no source plaintext or salt. Receipt access remains protected and retention/deletion policy is separate from outbox retry scheduling.

## Acceptance

`PG_BIN=... ./scripts/test-postgres.sh` tests real grants/reservations, concurrent immutable retries, scope/key/header substitution denial, terminal history, actual grant expiry, invalid admission timestamps and receipt/anchor/outbox/FSM rollback. `GRAMSTACK_TEST_CUSTODY=1 PG_BIN=... HYDRA_BIN=... ./scripts/test-hydra.sh` extends all previous gates with actual configured private mTLS custody, anonymous/wrong producer/public access denial, exact SQL durability and process restart. Add `GRAMSTACK_TEST_ESIGNET=1` for official eSignet. These custody fixtures sign scoped evidence with the registered test adapter key. The protected SDK/native data route has separate acceptance; custody alone does not prove source delivery.
