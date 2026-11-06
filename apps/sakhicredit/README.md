# SakhiCredit

Independent demo consumer product. This Go module uses the first-party Go application SDK, independently of gateway internals. It authenticates with Hydra, creates immutable platform requests, hands the person to the first-party consent portal, polls explicit progress, obtains a grant, and presents the original consumer certificate on the private data route. Credit review belongs here; no eligibility decision or product state is added to the gateway.

The Go binary embeds the responsive product UI and serves it at `/`; no Node runtime or asset build is needed in production. Headless acceptance drives the real application, identity/consent portal and source stack. With standalone sign-in configured, the product binds its encrypted session to a verified application-scoped GRAMStack identity. Browser sessions rotate after sign-in and expire on sign-out. Identity sign-in does not approve sharing or replace registry subject verification. Without that configuration, isolated legacy tests use browser possession only. Use fictional fixtures only until a real product access model is supplied.

## Configuration and run

Use a separate product PostgreSQL database and role, an approved Hydra consumer and registered consumer mTLS certificate. Set `SAKHI_DATABASE_URL`, then run the explicit migration:

```sh
GOWORK=off go run ./cmd/sakhicredit migrate
```

Startup verifies the embedded schema checksum and never applies migrations implicitly. Set `SAKHI_CONFIG_FILE` to a trusted operator JSON file:

```json
{
  "public_origin":"https://credit.example.org",
  "gateway_origin":"https://api.example.org",
  "gateway_data_origin":"https://gateway.railway.internal:8443",
  "hydra_token_url":"https://identity.example.org/oauth2/token",
  "client_id":"sakhicredit",
  "data_server_name":"gateway.gramstack.internal",
  "allow_loopback_http":false
}
```

Set secrets through restricted runtime variables/files:

- `SAKHI_CLIENT_SECRET`: Hydra client secret, backend only.
- `SAKHI_STORAGE_KEY_FILE`: exactly 32 random binary bytes, restricted permissions. Preserve this key across restarts; losing it makes retained input/results unreadable.
- `SAKHI_SERVICE_CA_FILE`, `SAKHI_CONSUMER_CERT_FILE`, `SAKHI_CONSUMER_KEY_FILE`: private data-service trust and registered consumer identity. Client certificates are never sent to public API/token endpoints.
- `PORT`: listener, default 8081.

Start with `GOWORK=off go run ./cmd/sakhicredit`. Without a config file, only liveness is available and readiness/API remain unavailable. HTTPS is required; literal loopback HTTP for the public/token origins additionally requires `allow_loopback_http=true` and `SAKHI_ENV=development`. The data origin always requires TLS with the pinned service DNS identity and deployment CA. Redirects cannot forward consumer credentials.

## Standalone application sign-in

Enroll a separate OpenID-only application client and exact callback in Hydra and
the gateway's `authentication.application_clients`. Add `login_issuer` (the exact
Hydra issuer) and `login_client_id` to the product JSON, and set
`SAKHI_LOGIN_CLIENT_SECRET` separately. Preserve the gateway's dedicated
`GRAMSTACK_APPLICATION_SUBJECT_KEY` across deployments for stable pairwise IDs.
The application client is distinct from `client_id`, which remains the consumer
client for data requests.

The Go SDK handles PKCE, state, nonce and ID-token validation. PostgreSQL stores
encrypted, expiring one-use login transactions and identity sessions. Migration 2
adds these tables without changing migration 1. The product's APIs require both
the identity session and its bound browser owner when sign-in is configured.

- `GET /api/identity`: whether sign-in is configured and this browser authenticated.
- `GET /auth/gramstack/login`: start browser-bound application sign-in.
- `GET /auth/gramstack/callback`: verify identity and rotate the product session.
- `POST /auth/gramstack/logout`: Origin/CSRF-protected session invalidation.

## Product API 0.1.0

All responses are `no-store`, same-origin, and carry `Sakhicredit-API-Version: 0.1.0`. GET `/api/session` issues/reuses a host-only HttpOnly session cookie and returns `csrf_token`. Production cookies use Secure and the `__Host-` prefix. POST operations require the exact configured Origin and matching `X-CSRF-Token`; there is no cross-origin API or browser-held OAuth/grant secret.

| Method and path | Behavior |
|---|---|
| POST `/api/applications` | Exact JSON `{ "member_reference":"member-12345", "include_savings":true }` plus one `Idempotency-Key`. Creates/reuses one browser-owned application and one platform request. |
| GET `/api/applications/{id}` | Local protected progress/result only; never calls the source or platform. |
| POST `/api/applications/{id}/refresh` | Empty body. Reconciles platform progress, or safely resumes an ambiguous request creation using its original idempotency key. |
| POST `/api/applications/{id}/retrieve` | Empty body. Requests a grant, commits one local fetch claim and retrieves once. A completed retry returns the already received product copy; failed/uncertain attempts cannot fetch again. |
| POST `/api/applications/{id}/cancel` | Empty body. Fences local retrieval before asking the platform to cancel; retries reconcile the same cancellation. |
| GET `/healthz`, `/readyz` | Process liveness and explicit migration/database readiness. |

SakhiCredit requests SHG membership and, if selected at creation, savings in INR for credit eligibility under `terms-v1`, single use and five-minute requested access. An existing request cannot change fields; cancel it and create a new application. Platform grant/admission checks remain authoritative. Product READY is a progress hint, never permission to bypass them.

## Persistence and recovery

A product-specific SQL FSM owns CONNECTING → WAITING → READY → FETCHING → COMPLETE, with cancellation, blocked/expired and UNKNOWN outcomes. Row locks/version checks fence concurrent actions. Fixed fetch deadlines and terminal state cannot be rewritten. A failed or ambiguous source call, lost completion checkpoint or abandoned fetch cannot silently regain a fetch slot. Native GET connection reuse is disabled to prevent implicit HTTP retries. A grant failure before the local claim can be retried with the same grant idempotency key; denied/expired grants close the product flow.

The product stores the locator and received result encrypted with AES-GCM, bound to application, owner and payload purpose. The platform never stores this product copy. Product idempotency input comparison uses a keyed digest. Session tokens are stored only as hashes; CSRF derives from the product key with a separate label. Exact requested fields, request/receipt binding, resource shape and decimal INR representation are checked before storing a result. The product commits its encrypted result before serving it to the browser, including after a disconnect where a checkpoint can still complete.

Applications/input/results are inaccessible after one hour; browser sessions expire after two hours, without sliding renewal. Maintenance physically removes at most 32 expired application rows and 32 unused expired sessions per pass. It also classifies at most 32 abandoned 45-second fetches as UNKNOWN. These TTLs are shorter than the maximum 30-day recipient terms; they do not prove backup/browser deletion. Cached product data represents an already admitted disclosure; withdrawal prevents future admission and does not retroactively erase a recipient copy. Downstream handling/deletion receipts are a separate D14 surface.

The reference process bounds active API operations to eight and overall requests to 35 seconds, with a separate five-second result checkpoint. Source data, grant JWTs, credentials and locator hints are absent from product logs and progress responses. Results remain protected by the browser's own session. Database/key/credential administration and production authentication remain deployment/product responsibilities.

## Acceptance

`GOWORK=off go test -race ./...` exercises strict client scope/result validation, token revocation recovery and redirect/origin denial. `PG_BIN=... ../../scripts/test-postgres.sh` also tests real SQL ownership, concurrent one-fetch claims, idempotency conflicts, encryption/key/AAD isolation, cancellation/terminal status, source/checkpoint failure, abandoned recovery, TTL enforcement and bounded physical cleanup.

`GRAMSTACK_TEST_PRODUCT=1 PG_BIN=... HYDRA_BIN=... ../../scripts/test-hydra.sh` runs the actual independent backend with Hydra, gateway, SDK adapter and fictional native source. It exercises explicit consent, protected approved result, rejection, cancellation, source outage and product/platform restart without a second native call. Add `GRAMSTACK_TEST_ESIGNET=1` for the isolated official provider; `GRAMSTACK_TEST_ANCHORING=1` under `test-dedi.sh` additionally enables actual DeDi. The full product flag implies every earlier integration gate. Browser acceptance is distinct from backend tests. Install the pinned development-only driver with `npm ci --ignore-scripts`, then use `GRAMSTACK_TEST_PRODUCT_UI=1` instead of the backend-only flag. It launches a fresh headless installed Chrome, preserving the user’s existing browser/profile, and checks desktop/mobile approval, exact fields, protected receipt download, reload, rejection, interrupted creation, network/cancellation, source outage and SQL expiry. `CHROME_BIN` can select the installed Chrome executable; no browser download is required. `GRAMSTACK_BROWSER_ARTIFACTS` selects the screenshot/redacted-result directory. Browser fault tests require the isolated loopback runner and reject production origins. Add the official eSignet flag to exercise its actual UIN/OTP UI. The default CI Hydra job runs the browser suite with the fictional fast IdP and retains redacted screenshots/report for seven days. The final playable recording remains a separate deliverable.
