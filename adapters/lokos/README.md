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
