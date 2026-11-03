# OIDC authentication adapter

Independent Go module: `github.com/theflywheel/GRAMStack/adapters/auth/oidc`.

`New(ctx, profile, credentials, callback, options)` constructs a pinned OIDC adapter. `Begin(transaction)` returns the authorization redirect; `Complete(code, transaction)` exchanges the code and returns a normalized `VerifiedPrincipal`. No citizen accounts, sessions, registry mappings or disclosure consent are stored here. Module release/publication is tracked separately in D13.

The caller owns random state/nonce/PKCE generation, browser binding, encrypted one-use transaction custody and consumption **before** calling Complete. The adapter constructs the S256 challenge, supplies the verifier, and validates signature, issuer, sole audience, nonce, azp, expiry, nbf, issuance and fresh signed auth_time. Authentication must be within five minutes. The caller limits session lifetime to the transaction and verified token expiry, and rechecks request state. A successful method call alone is not an authenticated browser session.

Example eSignet 2.0.0 local-demo public profile:

```json
{
  "schema_version": "gramstack.auth.oidc/v1",
  "issuer": "http://127.0.0.1:19080",
  "client_id": "registered-client-id",
  "token_endpoint_auth_method": "private_key_jwt",
  "assertion_key_id": "registered-key-id",
  "assertion_audience": "issuer",
  "id_token_algorithms": ["PS256"],
  "jwks_certificate_metadata": "ignore"
}
```

Credentials are supplied separately through `Credentials`: PKCS8 RSA private key (2048–8192 bits), or a client secret exclusively for `client_secret_basic`. Assertions use PS256, `iss = sub = client_id`, a fresh random jti, and a 60-second lifetime. The profile selects `issuer` or `token_endpoint` as assertion audience, never an arbitrary URL or fallback. The pinned eSignet OAuth dependency requires the issuer audience; its sample Postman collection's token endpoint audience does not work with this release.

Only literal loopback HTTP is allowed with explicit development permission. Normal deployments require HTTPS. Discovery endpoints are validated; token/discovery/JWKS redirects are refused. Errors expose no tokens, assertions, private keys or upstream response bodies.

The demo's official mock identity service can publish a negative-serial certificate in optional JWKS `x5c` metadata. The explicit `jwks_certificate_metadata: ignore` option requires `Options.AllowDemo`; it removes only optional certificate metadata from the exact discovered JWKS URL. The pinned issuer's public JWK fields still undergo normal JOSE signature verification. Duplicate/malformed/oversized key sets fail closed. Production uses the strict default and correctly generated provider certificates; there is no process-wide x509 override. Upstream `acr` is returned verbatim, with issuer provenance; absent `amr` stays absent. The adapter invents no assurance equivalence.

```sh
GOWORK=off go -C adapters/auth/oidc test -race ./...
GOWORK=off go -C adapters/auth/oidc vet ./...
# With the isolated official stack in infra/esignet running:
GRAMSTACK_TEST_ESIGNET=1 GOWORK=off go -C adapters/auth/oidc test -race -count=1 -run TestActualESignet -v ./...
```

The live test onboards an ephemeral public client key and fictional Sunita fixture, executes the official mock OTP flow, checks callback state, exchanges a real code through this adapter and verifies code replay denial. It only targets fixed loopback endpoints. Each run adds a client to the disposable local database; the fixture can already exist. This is an actual eSignet protocol integration test, with a mock identity backend. It is not yet the gateway/Hydra browser journey. Default fast CI runs the independent race and vet checks; the Docker-backed live test is opt-in locally.
