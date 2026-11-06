# GRAMStack JavaScript and TypeScript client

```sh
npm install https://github.com/theflywheel/GRAMStack/releases/download/client-sdks-v0.1.0/gramstack-client-0.1.0.tgz
```

This tarball installs `@gramstack/client`; it is distributed through GitHub
Releases rather than the npm registry.


Node.js 22+ backend SDK, with TypeScript declarations and a separate browser entry.

```js
import { Client } from "@gramstack/client";

const client = new Client({
  publicOrigin: gatewayOrigin, tokenURL, clientID, clientSecret,
  dataOrigin: privateDataOrigin, dataServerName,
  ca: caPEM, certificate: consumerCertPEM, key: consumerKeyPEM,
});
const accepted = await client.create("application-482", {
  registry: "lokos", resource: "shg.membership",
  subject_hint: {type: "member_reference", value: memberReference},
  fields: ["membership_status"], purpose: "credit_eligibility",
  access: {mode: "single_use", valid_for_seconds: 300},
  recipient_terms: {retention_policy_id: "terms-v1"},
});
// Return accepted.interaction_url to the browser.
const status = await client.status(accepted.request_id);
if (status.status === "CONSENTED") {
  const grant = await client.issueGrant(accepted.request_id, "application-482-grant");
  const result = await client.retrieve(grant, ["membership_status"]);
}
```

`waitForConsent(id,{timeout,interval,signal})` uses milliseconds and polls only
metadata. `cancel` stops unadmitted requests; `historyURL` returns the protected
person-facing history page. Credentials and retrieval grants stay on the backend.

```js
import { openConsent } from "@gramstack/client/browser";
// Invoke from a user gesture using the handoff returned by your backend.
openConsent(interactionURL, configuredGatewayOrigin);
```

Retrieval grants are opaque, locally one-attempt handles. The client disables
redirects and automatic retries. Keep durable attempt state across process
restarts. Use `rawJSON` with a decimal-aware decoder for exact numeric values.

## Standalone sign-in

```js
import { AuthClient } from "@gramstack/client";

const auth = await AuthClient.create({
  issuer: hydraIssuer, clientID: applicationClientID,
  clientSecret: applicationClientSecret, redirectURL: exactRegisteredCallback,
}, loginStore);
const authorizationURL = await auth.begin(serverOwnedBrowserBinding);
const identity = await auth.complete(sameBrowserBinding, exactCallbackURL);
```

The server keeps the binding in a Secure, HttpOnly, SameSite cookie.
`loginStore.saveLogin` persists encrypted TTL transactions and
`loginStore.takeLogin` atomically consumes only the matching browser-bound state.
The SDK verifies PKCE, nonce, state, signature, issuer, audience and fresh
authentication. The product establishes its session from the verified identity.
`MemoryLoginStore` is explicitly for single-process development.
