# Approved adapter discovery

The gateway validates public metadata before claiming a protected dispatch. `Approve` freezes a reviewed descriptor, copied P-256 keys and resource fields; it is not approval inferred from a remote manifest. `Verifier.Check` fetches only two fixed well-known paths at that approved origin, without consumer/service credentials or redirect following. It bounds body/time, enforces explicit core/discovery versions, rejects unknown/ambiguous fields and compares identity, chosen method, required capability/resource versions and exact approved key points. Newly advertised keys/resources never extend authority.

Root registry configuration now requires `discovery` for a data-enabled endpoint:

```json
{
  "registry":"lokos",
  "origin":"https://lokos-adapter.internal:8443",
  "server_name":"lokos-adapter.gramstack.internal",
  "verification_method":"demo.registry-map-v1",
  "verification_contract_version":"0.1.0",
  "demo":true,
  "discovery":{
    "version":"0.1.0",
    "origin":"https://adapter-metadata.example",
    "resources":[{"name":"shg.membership","version":"1","fields":["membership_status","savings_summary"]}]
  }
}
```

The private endpoint identity/audience and evidence public keys also remain bound by the existing `authorization.adapters` descriptor. Runtime joins them by registry and rejects disagreements. The public metadata origin may differ from the private data origin. HTTPS metadata uses system/deployment CA trust with hostname checks; only explicit development accepts literal-loopback HTTP. No manifest-controlled URL is followed.

Failure or mismatch returns ADAPTER_NOT_READY before SQL dispatch and performs no adapter data call. Restoring correct metadata permits safe retry of the still-unclaimed request. This is synchronous validation, not a cache or grant. Verification-only adapter hosting does not expose a full-retrieval manifest. See `specs/adapter-discovery/0.1.0` for the independently versioned public contract; immutable core 0.3.0 remains unchanged.

Race/SQL/process tests include unknown identities, key swaps/private fields, wrong resource version/method/capability/algorithm, canonical JSON, redirects, invalid origins, descriptor mutation, outage/no-claim/recovery and anonymous public metadata followed by actual protected disclosure. `GRAMSTACK_TEST_DISCOVERY=1` includes all earlier integration phases. This confirms fictional source behavior and configured infrastructure, not native LokOS onboarding or producer claims about arbitrary records.
