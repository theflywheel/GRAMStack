---
title: Application authentication
description: Versioned GRAMStack implementation contracts and conformance requirements.
---

# Standalone application authentication profile 0.1.0

This profile adds application-user sign-in alongside core 0.3.0. Hydra remains
the OIDC issuer and protocol engine. GRAMStack federates authentication to the
configured provider through its authentication adapter.

## Enrollment and trust

An operator registers a separate authorization-code client in Hydra and enrolls
the same client ID with exact callback URIs under
`authentication.application_clients` in the gateway configuration.
The application client is distinct from the portal and consumer machine clients.
Redirects require HTTPS, with explicit literal-loopback development exceptions.

The gateway requires a dedicated 32-byte
`GRAMSTACK_APPLICATION_SUBJECT_KEY` whenever application clients are enrolled.
A domain-separated HMAC over client ID, upstream issuer and subject yields an
application-scoped opaque subject. Preserve this key to preserve application
identities across encryption-key rotations. Raw upstream subjects are not
included in the issued application identity.

## Browser and token flow

1. The application's SDK persists a five-minute transaction containing random
   state and nonce, S256 PKCE verifier, start time and expiry. The store binds
   it to an unpredictable server-owned browser cookie.
2. The browser visits Hydra's authorization endpoint with `scope=openid`,
   `response_type=code`, nonce, S256 PKCE, exact callback, `prompt=login`
   and `max_age=0`.
3. Hydra sends its login challenge to the gateway's existing `/auth/login`.
   The gateway verifies the enrolled client, callback, original request and
   OpenID-only scope. A separate browser-bound transaction federates sign-in.
4. The existing `/auth/provider/callback` consumes that upstream transaction
   before exchanging the code. The authentication adapter verifies a fresh
   signed identity. The gateway accepts Hydra login with the application-scoped
   subject and records a short-lived, browser-bound consent correlation.
5. At `/auth/oauth-consent`, the gateway consumes that correlation and grants
   only `openid`, with an empty access-token audience. The signed ID token
   carries fresh `auth_time`.
6. Hydra redirects an authorization code to the registered application callback.
   The SDK consumes its own browser-bound state before code exchange and verifies
   the ID token signature, pinned issuer, sole client audience, nonce, PKCE,
   validity and fresh authentication. The product creates its own session.

State consumption must be atomic across replicas. Invalid browser bindings must
not consume another browser's transaction. Consumed or expired state cannot be
reused. Production state stores encrypt PKCE transactions and enforce expiry
against their authoritative clock. SDK memory stores are explicit single-process
development fixtures. Operators bound public login traffic and retained state.

## Authority separation

Application authentication yields a verified, application-scoped identity.
Consumer OAuth authenticates the registered organisation. Registry ownership
verification binds a person to a source record. Explicit disclosure consent
authorizes the exact data-sharing request. These remain separate checks.

Application clients grant neither `gramstack.access` nor offline access. Their
tokens cannot replace a registered consumer token, request-bound ownership proof,
explicit disclosure consent, retrieval grant or mTLS presenter identity.

## SakhiCredit integration

When its application-login profile is configured, SakhiCredit requires an
authenticated application identity for its application APIs. The SDK owns the
OIDC protocol and callback validation. SakhiCredit provides encrypted PostgreSQL
transaction storage and owns its session cookies and business authorization.

Successful sign-in rotates the browser owner and identity session. Existing
application authority and result rows remain immutable. Logout invalidates both
sessions. Identity sessions are bound to the browser owner and expire no later
than their verified ID token. Signing in does not start a data retrieval.
