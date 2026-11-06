import assert from "node:assert/strict"
import {readFileSync} from "node:fs"
import {randomBytes} from "node:crypto"
import test from "node:test"
import {Client,AuthClient,MemoryLoginStore,AlreadyAttempted,GramstackError,AuthenticationError,RemoteError} from "../src/index.js"
import {consentURL} from "../src/browser.js"

const config=JSON.parse(readFileSync(process.env.GRAMSTACK_SDK_FIXTURE,"utf8"))
const input={registry:"lokos",resource:"shg.membership",subject_hint:{type:"member_reference",value:"member-12345"},fields:["membership_status"],purpose:"credit_eligibility",access:{mode:"single_use",valid_for_seconds:300},recipient_terms:{retention_policy_id:"terms-v1"}}
async function control(path,body) {
 const response=await fetch(config.public_origin+"/_fixture/"+path,{method:body===undefined?"GET":"POST",headers:{"Content-Type":"application/json"},body:body===undefined?undefined:JSON.stringify(body)})
 return response.json()
}
function client() {
 return new Client({publicOrigin:config.public_origin,tokenURL:config.token_url,clientID:config.client_id,clientSecret:config.client_secret,
  dataOrigin:config.data_origin,dataServerName:config.data_server_name,ca:readFileSync(config.ca_file,"utf8"),
  certificate:readFileSync(config.certificate_file,"utf8"),key:readFileSync(config.key_file,"utf8"),allowLoopback:true})
}
function auth() {return AuthClient.create({issuer:config.issuer,clientID:"app-one",clientSecret:"fixture-secret",redirectURL:config.redirect_url,allowLoopback:true},new MemoryLoginStore())}
const binding=()=>randomBytes(32).toString("base64url")

test("lifecycle, token cache and one-attempt mTLS retrieval",async()=>{
 await control("reset",{})
 const sdk=client()
 const accepted=await sdk.create("test-create",input)
 assert.equal(consentURL(accepted.interaction_url,config.public_origin),accepted.interaction_url)
 assert.equal((await sdk.status(accepted.request_id)).status,"AUTHENTICATION_PENDING")
 const grant=await sdk.issueGrant(accepted.request_id,"test-grant")
 assert.ok(!JSON.stringify(grant).includes("fixture.grant.jwt"))
 const result=await sdk.retrieve(grant,input.fields)
 assert.deepEqual(result.data,{membership_status:"active"})
 assert.equal(result.receipt_id,"receipt-1")
 assert.equal(JSON.parse(result.rawJSON).receipt_id,"receipt-1")
 await assert.rejects(sdk.retrieve(grant,input.fields),AlreadyAttempted)
 assert.equal((await control("counts")).fetches,1)
 assert.equal((await control("counts")).tokens,1)
 assert.equal((await sdk.cancel(accepted.request_id)).status,"CANCELLED")
})
for(const mode of ["redirect","wrong_version","duplicate_json"]) test("reject "+mode,async()=>{
 await control("reset",{mode})
 await assert.rejects(client().status("request-1"),GramstackError)
 assert.equal((await control("counts")).traps,0)
})
test("reject foreign consent locator",async()=>{
 await control("reset",{mode:"foreign_locator"})
 await assert.rejects(client().create("test-create",input),GramstackError)
 assert.throws(()=>consentURL("https://untrusted.example/interactions/request-1",config.public_origin))
})
for(const mode of ["extra_field","disconnect"]) test("never retry "+mode,async()=>{
 await control("reset",{mode})
 const sdk=client()
 await sdk.create("test-create",input)
 const grant=await sdk.issueGrant("request-1","test-grant")
 await assert.rejects(sdk.retrieve(grant,input.fields),GramstackError)
 await assert.rejects(sdk.retrieve(grant,input.fields),AlreadyAttempted)
 assert.equal((await control("counts")).fetches,1)
})
test("concurrent calls share one attempt",async()=>{
 await control("reset",{})
 const sdk=client()
 await sdk.create("test-create",input)
 const grant=await sdk.issueGrant("request-1","test-grant")
 const results=await Promise.allSettled([sdk.retrieve(grant,input.fields),sdk.retrieve(grant,input.fields)])
 assert.equal(results.filter(value=>value.status==="fulfilled").length,1)
 assert.equal((await control("counts")).fetches,1)
})
test("401 invalidates cached token without replaying the request",async()=>{
 await control("reset",{mode:"unauthorized"})
 const sdk=client()
 await assert.rejects(sdk.status("request-1"),RemoteError)
 assert.equal((await control("counts")).tokens,1)
 await assert.rejects(sdk.status("request-1"),RemoteError)
 assert.equal((await control("counts")).tokens,2)
})
test("browser-bound one-use standalone sign-in",async()=>{
 await control("reset",{})
 const sdk=await auth(),browser=binding()
 const callback=(await control("authorize",{url:await sdk.begin(browser)})).callback
 await assert.rejects(sdk.complete(binding(),callback),AuthenticationError)
 assert.equal((await sdk.complete(browser,callback)).subject,"app_fixture_subject")
 await assert.rejects(sdk.complete(browser,callback),AuthenticationError)
 assert.equal((await control("counts")).exchanges,1)
})
for(const mode of ["wrong_nonce","wrong_audience","extra_audience","wrong_issuer","expired","invalid_signature","missing_auth_time"]) test("reject identity "+mode,async()=>{
 await control("reset",{mode})
 const sdk=await auth(),browser=binding()
 const callback=(await control("authorize",{url:await sdk.begin(browser)})).callback
 await assert.rejects(sdk.complete(browser,callback),GramstackError)
})
