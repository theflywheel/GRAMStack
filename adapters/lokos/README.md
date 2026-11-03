# LokOS reference adapter

Independent Go module using `github.com/theflywheel/GRAMStack/sdk/go/adapter`. It implements registry-native extension points; subject verification and strict field projection execute through the SDK. Native registry mechanics and credentials remain here, without a LokOS switch in the gateway.

The runtime has public liveness/unavailable-readiness endpoints and an optional private mTLS subject-verification listener. No protected business-data route is exposed until the grant/redemption/evidence pipeline is configured. This is not a claim that a real LokOS API has been integrated.

## Verification configuration

| Setting | Purpose |
|---|---|
| `ADAPTER_STATE_DATABASE_URL` | Adapter-local PostgreSQL custody |
| `ADAPTER_STATE_KEY` | Base64 random 32-byte encryption key, stable across instances/restarts |
| `SERVICE_CA_FILE`, `SERVICE_CERT_FILE`, `SERVICE_KEY_FILE` | Deployment CA and adapter service identity |
| `LOKOS_SOURCE_URL`, `LOKOS_SOURCE_SERVER_NAME` | Approved HTTPS native origin and pinned peer certificate name |
| `ADAPTER_TLS_PORT` | Private listener port, default 8443 |
| `ADAPTER_DEMO_VERIFIER=true` | Explicit fictional source integration; allowed only in development/demo |

Run `lokos-adapter migrate` explicitly before startup. `lokos-adapter` then serves the SDK verification profile 0.1.0 and requires the configured gateway identity. Its database pool is bounded. Startup does not perform migrations. Source responses marked demo are rejected unless demo permission is explicit; production configuration rejects the demo verifier before contacting dependencies.

`HTTPSource` implements the fictional native interface under `mocks/lokos`, not an assumed government LokOS endpoint. A real integration supplies approved source/verifier implementations and credentials within this adapter. The separate native mock owns identity-to-record fixtures, returns opaque mapping handles and remains inaccessible to the gateway's service certificate.

## Verification

`./scripts/check-go.sh` tests/vets independent modules and process shells. `GRAMSTACK_TEST_BINDING=1 PG_BIN=... HYDRA_BIN=... ./scripts/test-hydra.sh` exercises separate processes, mTLS boundaries, incorrect-member denial, concurrency, encrypted custody, rollback, cancellation, source failure and restart. Add `GRAMSTACK_TEST_ESIGNET=1` for the official eSignet variant with fictional native ownership and source data.

## Protected retrieval runtime

Set `ADAPTER_RETRIEVAL_CONFIG_FILE` and a separate PKCS8 `ADAPTER_EVIDENCE_SIGNING_KEY_FILE` to enable the SDK's service-mTLS POST `/internal/v1/retrievals`. The reference adapter supplies the provider/verifier/store and approved resource; all grant, attempt, projection, evidence and release processing uses the Go SDK. An explicit `migrate` installs both context and attempt schemas. No retrieval configuration keeps the runtime verification-only.

```json
{
  "grant_issuer":"https://gateway.example",
  "grant_audience":"lokos-adapter",
  "grant_keys":[{"key_id":"grant-v1","file":"/run/keys/grant.pub"}],
  "authority_origin":"https://gateway.internal:8444",
  "authority_service_identity":"gateway.gramstack.internal",
  "evidence_key_id":"adapter-key-v1",
  "release_profile":"batch",
  "max_concurrent":4
}
```

Private authority/custody calls use the adapter's service identity and pinned gateway server name. Evidence, grant and service signing keys must be distinct. The native client disables connection reuse to prevent implicit retries of its GET after an ambiguous disconnect; application-level retries are absent. Neither the gateway nor adapter serves cached business responses. The batch release profile requires real durable custody acknowledgement; a pending anchor does not claim DeDi publication. Shared conformance/discovery and real registry onboarding remain separate from the fictional native transport.
