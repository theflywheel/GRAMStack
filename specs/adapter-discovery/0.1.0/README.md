# Adapter discovery profile 0.1.0

This independent profile defines public capability/key publication and operator-approved discovery for core **0.3.0**. It does not change a published core contract. A separately hosted adapter may use any language; the Go SDK provides the reference metadata handler. [Schemas](schemas.json) and [examples](examples.json) validate message shape, not trust, signatures, curve membership or deployment readiness. Example keys establish no authority and MUST NOT be installed as production trust.

## Public endpoints

| Operation | Response | Authentication |
|---|---|---|
| GET `/.well-known/gramstack-adapter` | Core 0.3.0 AdapterManifest | Public metadata |
| GET `/.well-known/gramstack-adapter/jwks.json` | Profile PublicKeySet | Public verification keys only |

Successful responses MUST use JSON, `Cache-Control: no-store`, `Gramstack-Contract-Version: 0.3.0` and `Gramstack-Adapter-Discovery-Version: 0.1.0`. No people, records, private keys, source credentials, authority secrets or per-consumer policy are published. Advertised capabilities describe the configured protocol implementation, not a live availability guarantee. A verification-only runtime MUST NOT claim the full protected-retrieval capabilities.

The reference service publishes metadata on its public HTTP/HTTPS listener and keeps retrieval/verification RPCs on a separate mandatory-mTLS listener. Operator descriptors pin both origins and the private service identity. Production metadata requires HTTPS; explicit local development may use literal-loopback HTTP. Metadata does not provide a credential-forwarding or arbitrary-host lookup mechanism.

## Approved trust and validation

Before onboarding and before protected dispatch, the gateway MUST compare public metadata with an operator-approved descriptor. It pins registry, adapter ID, audience, contract/profile versions, authentication, required capabilities, chosen verification method, resource name/version/fields and approved public key material. Approval happens outside discovery. A fetched key or capability MUST NOT widen policy, select a new source host, add a trusted issuer or establish subject ownership.

The manifest MUST require `mtls`, `atomic-redemption` and `evidence-before-release`. It MUST contain the configured verification method and required resource versions/fields. Resource names MUST be unambiguous; duplicate names across versions are rejected because the core retrieval request selects a resource name. Signing key IDs and method lists are unique and bounded (16 each); resource declarations are bounded (32 resources, 128 unique fields each).

The JWK set is bounded to 16 unique advertised IDs. Each key MUST be public EC/P-256, `alg=ES256`, `use=sig`, with canonical unpadded 32-byte base64url coordinates on the curve. Each ID MUST occur in the manifest and its coordinates MUST exactly match approved trust. Private parameters, key URLs, unknown properties, ambiguous JSON, unsupported algorithms/curves and duplicate IDs fail closed. Retained approved public keys may outlive an active advertised signing key for historical verification; discovery never enrolls a new key automatically.

Clients MUST use fixed well-known paths under the approved metadata origin, bounded responses/timeouts, TLS hostname verification and no redirect following. They MUST NOT send consumer bearer tokens, cookies, source secrets or private service credentials to public discovery. Arbitrary URLs from manifests/JWKs are not followed.

## Failure semantics and conformance

Unknown, unavailable, malformed or mismatched metadata blocks protected dispatch before its SQL attempt claim. No adapter retrieval or source fetch occurs, and a request can retry after valid metadata is restored. Discovery is a synchronous gate; it is not a reservation and cannot authorize a disclosure. Current policy/consent, original sender, SDK validation, atomic redemption and evidence-before-release remain independently required.

Implementations MUST test anonymous metadata access; no private key/data leakage; exact approved identity, key point, method and resource binding; wrong contract/version/capability/curve/algorithm/use; duplicate/unknown fields and IDs; redirects and unapproved origins; outage recovery without an occupied dispatch; and successful protected retrieval after validation. Source mock controls and records remain separately hosted under `mocks/`, never inside the gateway or published production provider module.
