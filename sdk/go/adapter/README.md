# GRAMStack adapter Go SDK

Module: `github.com/theflywheel/GRAMStack/sdk/go/adapter` (Go 1.24+).

The SDK supplies registry-neutral resource projection, pinned mTLS service transport, strict JSON/canonical snapshot handling, registry-approved subject verification and optional encrypted PostgreSQL context custody through `database/sql`. The host supplies the SQL driver, approved native verifier, resource and explicit profile version; the SDK imports neither gateway internals nor registry code. LokOS uses these SDK extension points rather than duplicating the protocol implementation.

`NewVerification` requires contract version `VerificationVersion` (0.1.0), registry, gateway certificate identity, approved verification method, resource, SubjectVerifier and ContextStore. Its HTTP handler serves POST `/internal/v1/subject-verifications`. Both request and response pin `Gramstack-Subject-Verification-Version`. See the [versioned wire contract](../../../specs/subject-verification/0.1.0/README.md).

Verification recomputes the accepted snapshot digest, enforces supported fields and request/authentication expiry, and calls the registry's verifier. A locator and login alone never create ownership. Live retries converge on the same context, bound to request, principal and a private issuer/subject digest. A different person cannot reuse that cached binding. The native verifier returns an opaque source-owned mapping handle, not business data.

`NewSQLContextStore` requires a host-managed PostgreSQL connection and stable random 32-byte key. `Migrate` is an explicit, serialized, checksummed operator action; startup never runs DDL. Mapping payloads and their private principal digest are AES-GCM encrypted with authenticated request/context/digest/principal/expiry metadata. SQL enforces TTL on every read. Fresh verification may replace an expired tuple; live contexts cannot be mutated. Physical cleanup and overlapping key rotation are separate operator responsibilities. No production keys or database credentials are provided by this package.

`ReadSelected` is a projection primitive, not an authorization boundary. The protected grant/redemption/evidence pipeline must admit a disclosure before calling it. The reference runtime currently exposes the authenticated verification route and no business-data retrieval route. SDK release/publication and complete retrieval conformance are tracked in D08/D13.

Use the root Go workspace for local SDK + LokOS development. Independent check scripts use temporary module files before release; no local replace is committed in module files. Published SDK tags use `sdk/go/adapter/vX.Y.Z`.
