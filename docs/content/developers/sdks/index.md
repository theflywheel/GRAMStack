---
title: Application SDKs
---

GRAMStack provides Apache-2.0 application SDKs for Go, Python and JavaScript /
TypeScript. SakhiCredit uses the Go SDK for its backend integration and standalone
sign-in. The Python SDK has also been exercised through a local connector in a
separate FastAPI product.

| Language | Package | Runtime |
|---|---|---|
| Go | `github.com/theflywheel/GRAMStack/sdk/go/client` | Go 1.26+ |
| Python | `gramstack-client` (`import gramstack`) | Python 3.10+ |
| JavaScript / TypeScript | `@gramstack/client` | Node.js 22+ |

Install the Go module at `v0.1.0`. Python and JavaScript packages are downloadable
from the [SDK release](https://github.com/theflywheel/GRAMStack/releases/tag/client-sdks-v0.1.0);
they are not published on PyPI or the npm registry.

[Source, installation and examples](https://github.com/theflywheel/GRAMStack/tree/main/sdk)
include the independently publishable packages. The application SDKs are separate
from the Go adapter SDK used to implement registry integrations.

## Language guides

[Go](/developers/sdks/go) · [Python](/developers/sdks/python) · [JavaScript / TypeScript](/developers/sdks/javascript)

Each guide includes installation, request/retrieval examples and standalone sign-in. Start with the [integration quickstart](/developers/quickstart) for enrollment and operator prerequisites.

## Sign in with GRAMStack

Enroll an application OAuth client and its exact callback with the operator.
The SDK starts authorization-code sign-in with PKCE, state and nonce. GRAMStack
federates to eSignet, then returns a signed identity scoped to that application.
The SDK validates the issuer, audience, signature, nonce and authentication time.
The product creates its own session after verification.

Applications implement the SDK's shared, encrypted, browser-bound one-use login
store. SakhiCredit uses PostgreSQL, rotates the browser session after sign-in,
and invalidates its identity session on sign-out. In-memory stores are explicitly
for development in one process.

Sign-in creates no sharing request and grants no registry access. Consumer OAuth
credentials, registry subject verification, explicit human consent and the scoped
retrieval grant remain separate checks. See [[developers/specifications/profiles/application-auth]].

## Consent and retrieval

1. The backend authenticates with its registered consumer credentials.
2. It creates a request for exact fields with an idempotency key.
3. The browser opens the returned first-party interaction URL.
4. The person signs in, verifies registry ownership and approves or refuses.
5. The backend polls request metadata, obtains a grant after consent and performs
   one mTLS retrieval.
6. The product stores its authorized copy and exposes the receipt and audit trail.

Waiting for consent polls metadata only. Scope changes use cancellation and a
fresh request. Human decisions and withdrawal take place in the first-party
portal. The JavaScript browser helper only handles consent URLs; credentials and
mTLS keys belong on the backend.

## Delivery and data handling

SDKs refuse retrieval redirects and automatic retries. An opaque grant handle is
marked attempted before sending, including when delivery is uncertain. Products
must also persist their own retrieval attempt across restarts, as SakhiCredit
does. The server remains the authority for single-use admission.

Responses are bounded and validated for contract version, duplicate fields,
trusted locators and exact projection. Go preserves resource JSON, Python uses
`Decimal` for fractional values, and JavaScript exposes `rawJSON` for callers
that need a decimal-aware parser.

## Verification

The shared SDK conformance suite exercises all three languages with real TLS,
mTLS and signed OIDC fixtures. The SakhiCredit browser suite exercises Go SDK
integration with Hydra and separate identity, consent and retrieval flows.
Standalone acceptance checks callback replay, stable pairwise identities,
protected APIs, logout invalidation and a 390px mobile viewport.

[Python playground report](https://github.com/theflywheel/GRAMStack/blob/main/sdk/examples/python-playground.md)
records the pinned external revision and local results: 31 passed, one optional
live Bhashini test skipped. Its local connector covers the membership profile;
other product data sources need their own registered adapters and profiles.
