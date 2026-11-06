import {Client,AuthClient,MemoryLoginStore,AccessRequest} from "../src/index.js";
import {consentURL} from "../src/browser.js";
// @ts-expect-error The browser entry point must not expose the server credential-bearing client.
import {Client as BrowserClient} from "../src/browser.js";

async function contract(config: ConstructorParameters<typeof Client>[0], request: AccessRequest) {
  const client = new Client(config);
  const accepted = await client.create("application-1",request);
  consentURL(accepted.interaction_url,config.publicOrigin);
  const grant = await client.issueGrant(accepted.request_id,"grant-1");
  const result = await client.retrieve(grant,request.fields);
  const raw: string = result.rawJSON;
  // @ts-expect-error Retrieval requires an opaque SDK grant handle.
  await client.retrieve("raw-bearer-token",request.fields);
  // @ts-expect-error Core supports an explicit single-use profile.
  request.access.mode = "unlimited";
  const auth = await AuthClient.create({issuer:"https://issuer.example/",clientID:"app-one",redirectURL:"https://app.example/callback"},new MemoryLoginStore());
  const redirect: string = await auth.begin("server-owned-random-binding-with-entropy");
  return {raw,redirect};
}
void contract;
