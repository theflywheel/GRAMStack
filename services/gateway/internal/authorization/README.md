# Policy, grant issuance and admission

The authorization module implements the core 0.3.0 grant and redemption contracts. It does not fetch registry data. The adapter SDK owns the subsequent durable retrieval attempt, independent grant checks, exact projection and evidence-before-release.

## Deployment inputs

Configure `authorization` alongside authentication, registries and consent:

```json
{
  "key_id": "grant-key-v1",
  "listen_address": "0.0.0.0:8444",
  "policy_keys": [{"registry":"lokos","key_id":"lokos-policy-v1","file":"/run/keys/lokos-policy.pub"}],
  "policies": [{"file":"/run/policies/lokos.jws","expected_version":""}],
  "adapters": [{"registry":"lokos","audience":"lokos-adapter","service_identity":"lokos-adapter.gramstack.internal"}],
  "presenter_certificates": {"sakhi-credit":"/run/keys/sakhi-credit.pem"}
}
```

Issuer defaults to the public gateway origin. Set `consent.status_origin` to the private HTTPS authorization origin. Supply `GRAMSTACK_GRANT_SIGNING_KEY_FILE`, separate from the consent and service TLS keys; `SERVICE_CA_FILE`, `SERVICE_CERT_FILE` and `SERVICE_KEY_FILE` configure the private TLS listener. Presenter leaf certificates must chain to that deployment CA for client authentication. Restrict private ingress to participating services. TLS 1.3 requires verified client certificates; HTTP headers cannot establish a service identity.

Only the source public policy key is a runtime input. An offline operator can sign approved policy JSON with `gramstack sign-policy -input policy.json -key source.key -key-id lokos-policy-v1 -output policy.jws`. The output is created exclusively with mode 0600. Do not provision the source private key to a gateway deployment. Immutable signed policy versions and a current enabled head live in SQL. A replacement uses the expected current version. Old versions cannot be restored; reloading the current signed file preserves a disabled head. Runtime policy administration currently uses trusted Go methods/operator-controlled startup configuration, not a public HTTP API.

## API and trust boundary

- `POST /v1/access-requests/{id}/grant`: Hydra consumer authentication, empty body, one `Idempotency-Key`. Checks ownership, current policy, exact signed ACTIVE consent, subject context, recipient terms and deadlines. Returns one immutable ES256 compact grant per request. Expiry is the minimum of source policy, consent, subject and request bounds; lifetime is at most 300 seconds.
- `GET /.well-known/gramstack-jwks.json`: public verification keys only. This is a deployment key-discovery endpoint; an SDK must pin its trusted origin and issuer rather than trust a token-supplied URL.
- `POST /internal/v1/grants/redeem`: private verified adapter mTLS identity must own the grant audience. Request fields are the core `RedemptionRequest` contract. `presenter` is an attestation from that trusted adapter after authentication of the original presenter, never a browser claim or forwarded identity header. The SDK/gateway retrieval transport must establish that original certificate binding.
- `GET /internal/consents/{id}/status`: private adapter identity can inspect only consent associated with an issued grant for its audience. This is a status read, not admission permission.

Consumer authentication at issuance is Hydra OAuth. The grant binds the **registered** consumer certificate; original presenter proof must also be checked at retrieval. There is no assertion that holding a grant alone proves possession of that certificate.

## Transactions and retries

Preparation and signing happen outside SQL locks. Issuance commits under idempotency-key advisory lock → request row → current policy head shared lock → consent row. It revalidates authority after signing, then inserts immutable grant/idempotency records and the request FSM/outbox transition atomically. Existing grant retries do not need a functioning signer and do not extend expiry. Changed policy, subject context, terms or stopped consent deny reuse.

Redemption locks request → current policy head → consent, rechecks signature/scope and DB-clock bounds, then inserts the unique reservation, reserves the consent slot and advances the request to REDEEMING in one transaction. Withdrawal shares the consent row lock. A committed policy replacement/disable fences old grants. Failure of the request outbox rolls the whole admission back.

An identical admitted attempt returns its original metadata with `first_redemption=false`, including after expiry, withdrawal or policy revocation. This acknowledges a historical reservation; it confers no new source-read or data-release authority. Another attempt returns 409. The SDK must never repeat a fetch based on an acknowledgement retry. A source failure never restores the slot.

## Verification

`PG_BIN=... ./scripts/test-postgres.sh` runs real SQL concurrency, signer outage/substitution, source-key rejection, live policy replacement/disable, immutable retry, expiry, withdrawal races and rollback tests with race detection. `GRAMSTACK_TEST_AUTHZ=1 PG_BIN=... HYDRA_BIN=... ./scripts/test-hydra.sh` adds actual configured processes and TLS, anonymous/wrong peer and sender substitution denials, public/private route separation, malformed input, replay and restart recovery. Add `GRAMSTACK_TEST_ESIGNET=1` for official local eSignet with its fictional identity backend. No business-data retrieval or evidence release is claimed by this module alone.
