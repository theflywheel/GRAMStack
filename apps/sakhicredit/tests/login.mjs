import assert from "node:assert/strict";
import { existsSync,readFileSync } from "node:fs";
import { execFileSync } from "node:child_process";
import { chromium } from "playwright-core";

const origin=process.env.GRAMSTACK_TEST_PRODUCT_URL;
assert.equal(new URL(origin).hostname,"127.0.0.1","Isolated application-login acceptance only");
assert.equal(process.env.GRAMSTACK_TEST_PRODUCT_UI,"1");
const chrome=process.env.CHROME_BIN||["/Applications/Google Chrome.app/Contents/MacOS/Google Chrome","/usr/bin/google-chrome","/usr/bin/google-chrome-stable"].find(existsSync);
const browser=await chromium.launch({executablePath:chrome,headless:true});
const reads=()=>{
 const file=process.env.GRAMSTACK_TEST_SOURCE_READ_GATE+"/reads.log";
 return existsSync(file)?readFileSync(file,"utf8").trim().split("\n").filter(Boolean).length:0;
};
const requests=()=>Number(execFileSync(process.env.PG_BIN+"/psql",["-XAt",process.env.GRAMSTACK_TEST_DATABASE_URL,"-c","select count(*) from access_requests"],{encoding:"utf8"}).trim());
async function authenticate(page,landing){
 if(process.env.GRAMSTACK_TEST_ESIGNET==="1"){
  const mode=page.getByRole("button",{name:/log.*OTP|OTP.*log/i});
  const uin=page.getByRole("textbox",{name:/UIN|VID/i});
  await mode.or(uin).first().waitFor();
  if(!(await uin.first().isVisible()))await mode.click();
  await uin.fill("9000000001");
  await page.getByRole("button",{name:/get OTP/i}).click();
  const digits=page.locator('input[autocomplete="one-time-code"]');
  await digits.first().waitFor();
  for(let i=0;i<6;i++)await digits.nth(i).fill("1");
  await page.getByRole("button",{name:"Verify",exact:true}).click();
  const allow=page.getByRole("button",{name:/^Allow$/i});
  await landing.or(allow).first().waitFor();
  if(await allow.isVisible()){
   for(const checkbox of await page.getByRole("checkbox").all())await checkbox.check();
   await allow.click();
  }
 }else{
  await page.getByRole("button",{name:"Continue as Sunita Devi",exact:true}).click();
 }
 await landing.waitFor();
}
let currentPage;
const browserErrors=[];
try{
 const context=await browser.newContext({viewport:{width:390,height:844}});
 context.setDefaultTimeout(30000);
 const foreign=await browser.newContext();
 const page=await context.newPage();
 currentPage=page;
 page.on("console",message=>{
  if(message.type()==="error"&&/content security policy/i.test(message.text()))browserErrors.push("CSP blocked application authentication");
 });
 let callbackURL="";
 page.on("request",request=>{
  if(new URL(request.url()).pathname==="/auth/gramstack/callback")callbackURL=request.url();
 });
 await page.goto(origin);
 const session=await (await page.request.get(origin+"/api/session")).json();
 assert.equal((await page.request.post(origin+"/api/applications",{headers:{"Origin":origin,"X-CSRF-Token":session.csrf_token,"Idempotency-Key":"anonymous-denied"},data:{member_reference:"member-12345",include_savings:false}})).status(),401);
 const beforeRequests=requests(),beforeReads=reads();
 await page.getByRole("link",{name:"Sign in with GRAMStack",exact:true}).click();
 await authenticate(page,page.getByRole("button",{name:"Sign out",exact:true}));
 const identity=await (await page.request.get(origin+"/api/identity")).json();
 assert.equal(identity.authenticated,true);
 assert.match(identity.identity.subject,/^app_[A-Za-z0-9_-]{43}$/);
 assert.equal(requests(),beforeRequests,"standalone sign-in created disclosure authority");
 assert.equal(reads(),beforeReads,"standalone sign-in fetched registry data");
 assert.ok(callbackURL,"authorization callback was not observed");
 assert.equal((await foreign.request.get(callbackURL,{maxRedirects:0})).status(),403);
 assert.equal((await page.request.get(callbackURL,{maxRedirects:0})).status(),403,"callback replay accepted");
 await page.reload();
 await page.getByRole("button",{name:"Sign out",exact:true}).waitFor();
 assert.equal((await (await foreign.request.get(origin+"/api/identity")).json()).authenticated,false);
 assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),"mobile sign-in overflow");
 await page.getByLabel("Savings summary").uncheck();
 await page.getByRole("button",{name:/Start application/}).click();
 const popup=context.waitForEvent("page");
 await page.getByRole("link",{name:"Review sharing request"}).click();
 const portal=await popup;
 portal.setDefaultTimeout(30000);
 await authenticate(portal,portal.getByRole("button",{name:"Verify registry membership",exact:true}));
 await portal.getByRole("button",{name:"Verify registry membership",exact:true}).click();
 await portal.getByRole("heading",{name:"The exact notice",exact:true}).waitFor();
 await portal.getByRole("checkbox").check();
 await portal.getByRole("button",{name:"Approve this request",exact:true}).click();
 await portal.getByRole("heading",{name:"Your permission record",exact:true}).waitFor();
 await page.bringToFront();
 await page.getByRole("button",{name:"Get approved details"}).click();
 await page.getByRole("heading",{name:"Your details are ready for review"}).waitFor();
 assert.equal(await page.locator("#membership-value").textContent(),"Active member");
 assert.equal(reads(),beforeReads+1);
 const applicationID=await page.evaluate(()=>sessionStorage.getItem("sakhi.application"));
 const sessionCookies=await context.cookies(origin);
 const productCookie=sessionCookies.find(cookie=>cookie.name==="sakhi_demo");
 assert.ok(productCookie);
 await page.getByRole("button",{name:"Sign out",exact:true}).click();
 await page.locator("#sign-in-panel:not([hidden])").waitFor();
 assert.equal((await page.request.get(origin+"/api/applications/"+applicationID,{headers:{"Cookie":sessionCookies.map(cookie=>cookie.name+"="+cookie.value).join("; ")}})).status(),401);
 assert.equal((await (await page.request.get(origin+"/api/identity")).json()).authenticated,false);
 await page.getByRole("link",{name:"Sign in with GRAMStack",exact:true}).click();
 await authenticate(page,page.getByRole("button",{name:"Sign out",exact:true}));
 assert.equal((await (await page.request.get(origin+"/api/identity")).json()).identity.subject,identity.identity.subject,"application identity changed across sign-ins");
 assert.deepEqual(browserErrors,[]);
 console.log("PASS: standalone SDK sign-in, browser-bound callback replay denial, stable pairwise identity, protected product APIs, mobile layout, independent sharing consent, one retrieval and logout invalidation");
 await foreign.close();
 await context.close();
}catch(error){
 if(currentPage){
  console.error("Application login failure at",new URL(currentPage.url()).pathname);
  await currentPage.screenshot({path:"/tmp/gramstack-sdk-login-failure.png"});
 }
 throw error;
}finally{await browser.close()}
