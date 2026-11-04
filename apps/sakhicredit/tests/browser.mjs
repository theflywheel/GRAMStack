import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { chromium } from "playwright-core";
import { reportDemoLegalHold, showDemoLegalHold } from "./demo-handling.mjs";
import {
  existsSync,
  mkdirSync,
  readFileSync,
  writeFileSync,
  rmSync,
} from "node:fs";
import { execFileSync } from "node:child_process";
import { resolve, join } from "node:path";
import { tmpdir } from "node:os";

const origin = process.env.GRAMSTACK_TEST_PRODUCT_URL;
const gateway = process.env.GRAMSTACK_TEST_GATEWAY_URL;
const gate = process.env.GRAMSTACK_TEST_SOURCE_READ_GATE;
const database = process.env.GRAMSTACK_TEST_DATABASE_URL;
assert.equal(
  process.env.GRAMSTACK_TEST_PRODUCT_UI,
  "1",
  "Run through the isolated test-hydra.sh UI flag",
);
for (const url of [origin, gateway])
  assert.equal(
    new URL(url).hostname,
    "127.0.0.1",
    "Browser fault tests require isolated literal loopback origins",
  );
assert.match(
  new URL(database).searchParams.get("host"),
  /^\/(?:private\/)?tmp\/gramstack-hydra\./,
);
assert.ok(gate && process.env.PG_BIN);
const artifacts = resolve(
  process.env.GRAMSTACK_BROWSER_ARTIFACTS ||
    join(tmpdir(), "gramstack-sakhi-browser"),
);
console.log(`Browser artifacts: ${artifacts}`);
mkdirSync(artifacts, { recursive: true });
for (const file of [
  "authentication-csp.txt",
  ...Array.from({ length: 12 }, (_, i) => `failure-${i + 1}.png`),
  ...Array.from({ length: 12 }, (_, i) => `failure-${i + 1}.txt`),
])
  rmSync(join(artifacts, file), { force: true });
rmSync(join(artifacts, "acceptance.json"), { force: true });
const chrome =
  process.env.CHROME_BIN ||
  [
    "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
    "/usr/bin/google-chrome",
    "/usr/bin/google-chrome-stable",
  ].find(existsSync);
assert.ok(chrome, "Set CHROME_BIN to an installed supported Chrome executable");
const reads = () => {
  const path = join(gate, "reads.log");
  return existsSync(path)
    ? readFileSync(path, "utf8").split("\n").filter(Boolean).length
    : 0;
};
const browser = await chromium.launch({
  executablePath: chrome,
  headless: true,
  args: ["--disable-dev-shm-usage"],
});
const evidence = {
  browser: await browser.version(),
  cases: [],
  screenshots: [],
};
const deadline = new Promise((_, reject) => {
  globalThis.acceptanceTimer = setTimeout(
    () => reject(new Error("Browser acceptance exceeded 180 seconds")),
    180000,
  );
});
const pendingResponses = [],
  responseFailures = [];
async function shot(page, name) {
  await page.screenshot({
    path: join(artifacts, `${name}.png`),
    fullPage: true,
  });
  evidence.screenshots.push(`${name}.png`);
}
async function layout(page) {
  assert.equal(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
    true,
    "Horizontal overflow",
  );
}
async function casePage(mobile = false) {
  console.log(`Browser scenario ${evidence.cases.length + 1}`);
  const context = await browser.newContext({
    viewport: mobile
      ? { width: 390, height: 844 }
      : { width: 1400, height: 1100 },
    reducedMotion: "reduce",
  });
  context.setDefaultTimeout(15000);
  const page = await context.newPage();
  page.setDefaultTimeout(15000);
  page.on("pageerror", () =>
    responseFailures.push("Product page JavaScript error"),
  );
  page.on("response", (response) => {
    if (!response.url().startsWith(`${origin}/api/`)) return;
    const check = (async () => {
      const raw = await response.text();
      assert.doesNotMatch(
        raw,
        /"(?:grant|access_token|client_secret|member_reference)"/,
      );
      assert.equal(response.headers()["cache-control"], "no-store");
      assert.equal(response.request().headers()["authorization"], undefined);
    })().catch((error) => responseFailures.push(error.message));
    pendingResponses.push(check);
  });
  await page.goto(origin);
  await page
    .getByRole("button", { name: "Start application", exact: false })
    .waitFor();
  await page.locator("#start:not([disabled])").waitFor();
  await layout(page);
  return { page, context };
}
async function create(page, savings = true) {
  if (!savings) await page.getByLabel("Savings summary").uncheck();
  await page
    .getByRole("button", { name: "Start application", exact: false })
    .click();
  await page.getByRole("link", { name: "Review sharing request" }).waitFor();
  const id = await page.evaluate(() =>
    sessionStorage.getItem("sakhi.application"),
  );
  const response = await page.request.get(`${origin}/api/applications/${id}`);
  assert.equal(response.status(), 200);
  const app = await response.json();
  assert.equal(app.state, "WAITING");
  return app;
}
async function notice(page, context) {
  const opened = context.waitForEvent("page", { timeout: 15000 });
  await page.getByRole("link", { name: "Review sharing request" }).click();
  const portal = await opened;
  portal.setDefaultTimeout(20000);
  portal.on("console", (message) => {
    if (
      message.type() === "error" &&
      /content security policy/i.test(message.text())
    )
      writeFileSync(
        join(artifacts, "authentication-csp.txt"),
        message.text().replace(/https?:\/\/[^\s'")]+/g, (raw) => {
          try {
            const u = new URL(raw);
            return u.origin + u.pathname;
          } catch {
            return "[redacted URL]";
          }
        }),
      );
  });
  if (process.env.GRAMSTACK_TEST_ESIGNET === "1") {
    const mode = portal.getByRole("button", { name: /log.*OTP|OTP.*log/i });
    const uin = portal.getByRole("textbox", { name: /UIN|VID/i });
    await mode.or(uin).first().waitFor();
    if (!(await uin.first().isVisible())) await mode.click();
    await uin.fill("9000000001");
    await portal.getByRole("button", { name: /get OTP/i }).click();
    const digits = portal.locator('input[autocomplete="one-time-code"]');
    await digits.first().waitFor();
    assert.equal(await digits.count(), 6);
    if (!evidence.screenshots.includes("esignet-otp.png"))
      await shot(portal, "esignet-otp");
    for (let i = 0; i < 6; i++) await digits.nth(i).fill("1");
    await portal.getByRole("button", { name: "Verify", exact: true }).click();
    const registry = portal.getByRole("button", {
      name: "Verify registry membership",
      exact: true,
    });
    const allow = portal.getByRole("button", { name: /^Allow$/i });
    await registry.or(allow).first().waitFor();
    if (await allow.isVisible()) {
      for (const checkbox of await portal.getByRole("checkbox").all())
        await checkbox.check();
      await allow.click();
    }
  } else {
    await portal
      .getByRole("button", { name: "Continue as Sunita Devi", exact: true })
      .click();
  }
  await portal
    .getByRole("button", { name: "Verify registry membership", exact: true })
    .click();
  await portal
    .getByRole("heading", { name: "The exact notice", exact: true })
    .waitFor();
  return portal;
}
async function approve(page, context) {
  const portal = await notice(page, context);
  const shown = {
    text: await portal.locator(".notice-text").textContent(),
    presentationID: await portal.locator('input[name="presentation_id"]').first().inputValue(),
    version: await portal.locator('input[name="notice_version"]').first().inputValue(),
  };
  await portal.getByRole("checkbox").check();
  await portal
    .getByRole("button", { name: "Approve this request", exact: true })
    .click();
  await portal
    .getByRole("heading", { name: "Your permission record", exact: true })
    .waitFor();
  // Leave the consent tab open until context teardown. Closing an active
  // popup during a navigation can stall Chromium's target-close command.
  await page.bringToFront();
  await page.getByRole("button", { name: "Get approved details" }).waitFor();
  return shown;
}
async function run() {
  {
    const { page, context } = await casePage();
    await shot(page, "desktop-start");
    const started = await create(page);
    await shot(page, "desktop-waiting");
    const shown = await approve(page, context);
    assert.match(shown.text, /Processing profile: sakhi-credit-assessment \(version 1\)/);
    assert.match(shown.text, /Responsible organisation: sakhi-credit/);
    assert.match(shown.text, /Declared processors: None declared/);
    assert.match(shown.text, /Purpose manifest: credit_eligibility \(version 1\)/);
    assert.match(shown.text, /Purpose owner: sakhi-credit/);
    assert.match(shown.text, /membership_status: Check eligible SHG membership/);
    await shot(page, "desktop-ready");
    const before = reads();
    for (const file of ["block", "entered", "release"])
      rmSync(join(gate, file), { force: true });
    writeFileSync(join(gate, "block"), "controlled source latency", {
      mode: 0o600,
    });
    try {
      await page.getByRole("button", { name: "Get approved details" }).click();
      await page
        .getByRole("heading", { name: "Requesting your approved details" })
        .waitFor();
      const until = Date.now() + 5000;
      while (!existsSync(join(gate, "entered")) && Date.now() < until)
        await new Promise((resolve) => setTimeout(resolve, 25));
      assert.equal(
        existsSync(join(gate, "entered")),
        true,
        "Native call did not reach the controlled latency gate",
      );
      assert.equal(await page.locator("#retrieve").isDisabled(), true);
      await shot(page, "sharing-in-progress");
      writeFileSync(join(gate, "release"), "release", { mode: 0o600 });
      await page
        .getByRole("heading", { name: "Your details are ready for review" })
        .waitFor();
    } finally {
      for (const file of ["block", "entered", "release"])
        rmSync(join(gate, file), { force: true });
    }
    assert.equal(
      await page.locator("#membership-value").textContent(),
      "Active member",
    );
    assert.equal(
      await page.locator("#savings-value").textContent(),
      "₹12,500.75",
    );
    assert.equal(reads(), before + 1);
    await page.reload();
    await page
      .getByRole("heading", { name: "Your details are ready for review" })
      .waitFor();
    assert.equal(reads(), before + 1, "Reload refetched source data");
    const storage = await page.evaluate(() => ({
      local: Object.keys(localStorage),
      session: Object.keys(sessionStorage),
      readableSession: document.cookie.includes("sakhi_demo="),
    }));
    assert.deepEqual(storage.local, []);
    assert.deepEqual(storage.session, ["sakhi.application"]);
    assert.equal(storage.readableSession, false);
    await layout(page);
    await shot(page, "desktop-result");
    await page.setViewportSize({ width: 390, height: 844 });
    await layout(page);
    await shot(page, "mobile-both-fields");
    await page.setViewportSize({ width: 1400, height: 1100 });
    await page.getByText("View sharing reference", { exact: true }).click();
    const receiptPage = context.waitForEvent("page", { timeout: 15000 });
    await page.getByRole("link", { name: "View consent receipt" }).click();
    const receipt = await receiptPage;
    await receipt
      .getByRole("heading", { name: "Your permission record", exact: true })
      .waitFor();
    assert.equal(
      new URL(receipt.url()).pathname,
      `/interactions/${started.request_id}/receipt`,
    );
    reportDemoLegalHold(
      process.env.GRAMSTACK_TEST_AUTHZ_URL,
      process.env.GRAMSTACK_TEST_CERTS,
      await page.locator("#receipt-reference").textContent(),
    );
    await showDemoLegalHold(receipt);
    await shot(receipt, "recipient-handling-report");
    const downloading = receipt.waitForEvent("download", { timeout: 5000 });
    await receipt
      .getByRole("link", { name: "Download your record", exact: true })
      .click();
    const downloaded = await downloading;
    const recordPath = join(artifacts, "private-download.json");
    try {
      await downloaded.saveAs(recordPath);
      const record = JSON.parse(readFileSync(recordPath, "utf8"));
      assert.equal(record.request_id, started.request_id);
      const digest = "sha256:" + createHash("sha256").update(shown.text).digest("hex");
      assert.equal(record.notice.text, shown.text);
      assert.equal(record.notice.notice_digest, digest);
      assert.equal(record.notice.notice_version, shown.version);
      assert.equal(record.notice.language, "en");
      assert.equal(record.decision.decision, "approve");
      assert.equal(record.decision.explicit_confirmation, true);
      const presentation = record.decision.presentation;
      assert.equal(presentation.presentation_id, shown.presentationID);
      assert.equal(presentation.notice_digest, digest);
      assert.equal(presentation.notice_version, shown.version);
      assert.equal(presentation.language, "en");
      assert.equal(presentation.channel, "WEB");
      const rendered = Date.parse(presentation.rendered_at);
      const decided = Date.parse(record.decision.decided_at);
      assert.ok(rendered <= decided && decided < Date.parse(presentation.expires_at));
    } finally {
      rmSync(recordPath, { force: true });
    }
    assert.equal(new URL(receipt.url()).origin, gateway);
    assert.equal(reads(), before + 1);
    await page.bringToFront();
    await page
      .getByRole("button", { name: "Start a new application", exact: true })
      .click();
    await page.locator("#start:not([disabled])").waitFor();
    assert.equal(await page.locator("#approved-result").isHidden(), true);
    evidence.cases.push(
      "approved result, one native read, encrypted reload recovery, protected receipt with exact rendered notice provenance, recipient report and browser secret isolation",
    );
    await context.close();
  }
  {
    const { page, context } = await casePage(true);
    await shot(page, "mobile-start");
    await create(page, false);
    await approve(page, context);
    const before = reads();
    await page.getByRole("button", { name: "Get approved details" }).click();
    await page
      .getByRole("heading", { name: "Your details are ready for review" })
      .waitFor();
    assert.equal(await page.locator("#savings-tile").isHidden(), true);
    assert.equal(reads(), before + 1);
    await layout(page);
    await shot(page, "mobile-result");
    evidence.cases.push("mobile layout and membership-only field selection");
    await context.close();
  }
  {
    const { page, context } = await casePage();
    const before = reads();
    await create(page);
    const portal = await notice(page, context);
    await portal
      .getByRole("button", { name: "Reject this request", exact: true })
      .click();
    await portal
      .getByRole("heading", { name: "Your permission record", exact: true })
      .waitFor();
    await page.bringToFront();
    await page.getByRole("heading", { name: "Sharing declined" }).waitFor();
    assert.equal(reads(), before);
    assert.equal(await page.locator("#retrieve").isHidden(), true);
    await shot(page, "rejected");
    evidence.cases.push("explicit rejection with no source read");
    await context.close();
  }
  {
    const { page, context } = await casePage();
    const before = reads();
    let originalID;
    const fault = Promise.withResolvers();
    await page.route(
      `${origin}/api/applications`,
      async (route) => {
        try {
          const response = await route.fetch();
          originalID = (await response.json()).id;
          await route.abort("failed");
          fault.resolve();
        } catch (error) {
          fault.reject(error);
        }
      },
      { times: 1 },
    );
    await Promise.all([
      page
        .getByRole("button", { name: "Start application", exact: false })
        .click(),
      fault.promise,
    ]);
    await page.getByRole("alert").waitFor();
    assert.equal(
      await page
        .getByLabel("SHG member reference", { exact: true })
        .isDisabled(),
      true,
    );
    await page.getByRole("button", { name: "Try again", exact: true }).click();
    await page.getByRole("link", { name: "Review sharing request" }).waitFor();
    assert.equal(
      await page.evaluate(() => sessionStorage.getItem("sakhi.application")),
      originalID,
    );
    await page
      .getByRole("button", { name: "Cancel this application", exact: true })
      .click();
    await page
      .getByRole("heading", { name: "Application cancelled" })
      .waitFor();
    assert.equal(reads(), before);
    evidence.cases.push(
      "lost creation response recovers the same application and immutable scope",
    );
    await context.close();
  }
  {
    const { page, context } = await casePage();
    const before = reads();
    await create(page);
    await context.setOffline(true);
    await page
      .getByRole("button", { name: "Check progress", exact: true })
      .click();
    await page.getByRole("alert").waitFor();
    await context.setOffline(false);
    await page.getByRole("button", { name: "Try again", exact: true }).click();
    await page.locator("#connection-message").waitFor({ state: "hidden" });
    await page
      .getByRole("button", { name: "Cancel this application", exact: true })
      .click();
    await page
      .getByRole("heading", { name: "Application cancelled" })
      .waitFor();
    assert.equal(reads(), before);
    await shot(page, "cancelled");
    evidence.cases.push(
      "network interruption, same-application recovery and cancellation",
    );
    await context.close();
  }
  {
    const { page, context } = await casePage();
    await create(page);
    await approve(page, context);
    writeFileSync(join(gate, "offline"), "isolated browser fault", {
      mode: 0o600,
    });
    try {
      await page.getByRole("button", { name: "Get approved details" }).click();
      await page
        .getByRole("heading", { name: "We couldn’t confirm the result" })
        .waitFor();
    } finally {
      rmSync(join(gate, "offline"), { force: true });
    }
    const before = reads();
    await page.reload();
    await page
      .getByRole("heading", { name: "We couldn’t confirm the result" })
      .waitFor();
    assert.equal(reads(), before);
    assert.equal(await page.locator("#retrieve").isHidden(), true);
    await shot(page, "source-outage");
    evidence.cases.push(
      "actual source outage and no retrieval retry after reload",
    );
    await context.close();
  }
  {
    const { page, context } = await casePage();
    const app = await create(page);
    const before = reads();
    assert.match(app.request_id, /^[A-Za-z0-9_.:-]{1,128}$/);
    execFileSync(
      join(process.env.PG_BIN, "psql"),
      [
        database,
        "-X",
        "-q",
        "-v",
        "ON_ERROR_STOP=1",
        "-v",
        `rid=${app.request_id}`,
      ],
      {
        input:
          "UPDATE access_requests SET expires_at=clock_timestamp()-interval '1 second' WHERE request_id=:'rid';",
        stdio: ["pipe", "ignore", "pipe"],
      },
    );
    await page
      .getByRole("button", { name: "Check progress", exact: true })
      .click();
    await page
      .getByRole("heading", { name: "The sharing request has expired" })
      .waitFor();
    assert.equal(reads(), before);
    await shot(page, "expired");
    evidence.cases.push(
      "authoritative SQL request expiry surfaced without a source read",
    );
    await context.close();
  }
  await Promise.all(pendingResponses);
  assert.deepEqual(responseFailures, []);
  writeFileSync(
    join(artifacts, "acceptance.json"),
    JSON.stringify(evidence, null, 2) + "\n",
  );
  console.log(
    `PASS: ${evidence.cases.length} real browser scenarios; ${evidence.screenshots.length} screenshots in ${artifacts}`,
  );
}
try {
  await Promise.race([run(), deadline]);
} catch (error) {
  let n = 0;
  for (const context of browser.contexts())
    for (const page of context.pages()) {
      try {
        await shot(page, `failure-${++n}`);
        writeFileSync(
          join(artifacts, `failure-${n}.txt`),
          `${new URL(page.url()).origin}${new URL(page.url()).pathname}\n${await page.locator("body").innerText()}`,
        );
        writeFileSync(
          join(artifacts, `failure-${n}-controls.json`),
          JSON.stringify(
            await page
              .locator("input")
              .evaluateAll((inputs) =>
                inputs.map((i) => ({
                  id: i.id,
                  name: i.name,
                  type: i.type,
                  placeholder: i.placeholder,
                  autocomplete: i.autocomplete,
                  label: i.getAttribute("aria-label"),
                  maxLength: i.maxLength,
                })),
              ),
            null,
            2,
          ),
        );
      } catch {}
    }
  throw error;
} finally {
  clearTimeout(globalThis.acceptanceTimer);
  if (existsSync(join(gate, "block")))
    writeFileSync(join(gate, "release"), "release", { mode: 0o600 });
  await browser.close();
  for (const file of ["offline", "block", "entered", "release"])
    rmSync(join(gate, file), { force: true });
}
