# Application SDKs

First-party application integration for GRAMStack core 0.3.0 and standalone
application authentication profile 0.1.0. Each consumer SDK is Apache-2.0 licensed.

| Language | Package | Runtime |
|---|---|---|
| Go | `github.com/theflywheel/GRAMStack/sdk/go/client` | Go 1.26+ |
| Python | `gramstack-client`, imported as `gramstack` | Python 3.10+ |
| JavaScript / TypeScript | `@gramstack/client` | Node.js 22+ |
| Browser helper | `@gramstack/client/browser` | Modern browsers |

The application SDKs are separate from the registry [Go adapter SDK](go/adapter/).
Applications authenticate with consumer OAuth, request explicit fields, hand the
person to GRAMStack's consent portal, observe status, obtain an opaque retrieval
grant, and make one mTLS retrieval. SDKs validate contract versions, pinned
locators and exact field projection.

Standalone application sign-in uses enrolled Hydra clients and eSignet federation.
SDKs manage state, nonce, PKCE, code exchange, signature and claim checks through a
browser-bound one-use state-store contract. The returned identity is scoped to the
application. Sign-in does not approve disclosure or issue a consumer data token.
The application owns its authenticated session and business authorization.

## Integration sequence

1. Configure the registered consumer, public gateway, Hydra token endpoint,
   and pinned mTLS retrieval endpoint.
2. Call `create` with a stable idempotency key and the exact request.
3. Present the returned `interaction_url` to the person.
4. Poll metadata using `status` or a bounded `waitForConsent` /
   `wait_for_consent` / `WaitForConsent`.
5. On `CONSENTED`, issue a grant with a stable key and retrieve the exact fields.
6. Retain the receipt reference and handle the data under the agreed terms.
   The SDK does not persist business records.

Scope changes use cancellation and a fresh request. Human approval and withdrawal
happen in the authenticated first-party portal. The SDK provides a protected
history URL for the person's records.

## Authentication and retry boundaries

Consumer credentials and mTLS keys belong on the application's backend. The
JavaScript browser entry point only validates and opens consent handoffs.

Retrieval has no automatic retries, redirects, speculative fetch or shared cache.
The SDK marks its opaque grant handle attempted before sending. A disconnect,
timeout or malformed response keeps that handle spent. The server remains the
authority across process restarts. Keep your application's own durable
retrieval-attempt record, as demonstrated by SakhiCredit, and inspect platform
status after an uncertain outcome.

Create and grant calls use caller-supplied idempotency keys. SDKs invalidate
revoked cached OAuth tokens but leave retries explicit. Consent waiting polls
only metadata and never initiates retrieval.

Production sign-in requires a shared store that encrypts PKCE transactions,
checks database expiry, and atomically consumes state only for the initiating
browser binding. The explicit memory stores are for development in one process.
SakhiCredit supplies PostgreSQL storage and rotates its application session after
successful sign-in.

JavaScript's `data` uses native numbers. Use `rawJSON` with a decimal-aware parser
where exact numeric representation matters. Python preserves fractional numbers
as `Decimal`; Go retains resource fields as `json.RawMessage`.

## Validation

`scripts/test-client-sdks.sh` runs the same isolated TLS/OIDC fixtures across all
three languages. The live Hydra/eSignet suites exercise the SDK-backed SakhiCredit
product and standalone sign-in. The Python playground checkout is tested locally,
with no upstream pull request or remote change.
