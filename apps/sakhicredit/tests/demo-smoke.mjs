import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { chromium } from "playwright-core";
import { setTimeout as delay } from "node:timers/promises";
import { captureDemo } from "./demo-capture.mjs";
import {
  acknowledgeDemoCorrection, reportDemoLegalHold, showDemoLegalHold,
} from "./demo-handling.mjs";
const state = process.argv[2];
assert.ok(
  state && process.env.CHROME_BIN,
  "Pass the demo state directory and set CHROME_BIN",
);
const hosted = process.env.GRAMSTACK_DEMO_HOSTED === "1";
const endpoints = hosted
  ? {
      product: "https://sakhicredit-b9b69819.up.railway.app",
      gateway: "https://gramstack-gateway-b9b69819.up.railway.app",
    }
  : JSON.parse(readFileSync(join(state, "endpoints.json"), "utf8"));
assert.equal(new URL(endpoints.product).origin, endpoints.product);
if (!hosted) assert.equal(new URL(endpoints.product).hostname, "127.0.0.1");
const browser = await chromium.launch({
  executablePath: process.env.CHROME_BIN,
  headless: true,
  timeout: 15000,
});
const output = process.env.GRAMSTACK_DEMO_RECORDING;
const recording = output ? captureDemo(output) : undefined;
const hold = async (milliseconds) => {
  if (recording) await delay(milliseconds);
};
async function showCapabilities(page) {
  const report = readFileSync(
    new URL("../../../DEMO-CAPABILITIES.md", import.meta.url),
    "utf8",
  );
  const rows = report
    .split("\n")
    .filter((line) => line.startsWith("| "))
    .slice(1)
    .map((line) =>
      line
        .slice(1, -1)
        .split("|")
        .map((cell) => cell.trim().replace(/\[([^\]]+)\]\([^)]+\)/g, "$1")),
    );
  assert.ok(rows.length > 0 && rows.every((row) => row.length === 3));
  const rowsPerCard = 3;
  for (let index = 0; index < rows.length; index += rowsPerCard) {
    const segment = rows.slice(index, index + rowsPerCard);
    await page.setContent(`<!doctype html><meta charset="utf-8">
      <title>SakhiCredit capability report</title>
      <style>
      body{margin:0;background:#f5f7f3;color:#19382e;font:19px system-ui}
      main{margin:36px auto;max-width:1120px;padding:30px;background:white;border-radius:20px}
      h1{font-size:42px;margin:10px 0 22px}
      table{width:100%;border-collapse:collapse}
      th,td{text-align:left;vertical-align:top;padding:14px 12px;border-bottom:1px solid #dce6de}
      td:first-child{width:22%;font-weight:600}td:nth-child(2){width:20%}
      </style>
      <main><p>SakhiCredit · ${hosted ? "Hosted demo" : "Local rehearsal"} · ${index / rowsPerCard + 1} of ${Math.ceil(rows.length / rowsPerCard)}</p>
      <h1>What this demo verifies</h1>
      <table><thead><tr><th>Capability</th><th>Status</th><th>Evidence and boundary</th></tr></thead><tbody></tbody></table></main>`);
    await page.locator("tbody").evaluate((body, values) => {
      for (const row of values) {
        const tr = body.insertRow();
        for (const cell of row) tr.insertCell().textContent = cell;
      }
    }, segment);
    assert.equal(
      await page.evaluate(
        () => document.documentElement.scrollHeight <= innerHeight,
      ),
      true,
      "Capability card must fit the recorded viewport",
    );
    await hold(7000);
  }
}

async function signIn(portal, landing) {
  const mode = portal.getByRole("button", { name: /log.*OTP|OTP.*log/i });
  const uin = portal.getByRole("textbox", { name: /UIN|VID/i });
  await mode.or(uin).first().waitFor();
  if (!(await uin.first().isVisible())) await mode.click();
  await recording?.show(portal);
  await hold(1600);
  recording?.pause();
  await uin.fill("9000000001");
  await portal.getByRole("button", { name: /get OTP/i }).click();
  const digits = portal.locator('input[autocomplete="one-time-code"]');
  await digits.first().waitFor();
  assert.equal(await digits.count(), 6);
  for (let i = 0; i < 6; i++) await digits.nth(i).fill("1");
  await portal.getByRole("button", { name: "Verify", exact: true }).click();
  const allow = portal.getByRole("button", { name: /^Allow$/i });
  await landing.or(allow).first().waitFor();
  if (await allow.isVisible()) {
    for (const checkbox of await portal.getByRole("checkbox").all())
      await checkbox.check();
    await allow.click();
  }
  await landing.waitFor();
}

async function review() {
  recording?.pause();
  const context = await browser.newContext({
    viewport: { width: 1280, height: 800 },
  });
  context.setDefaultTimeout(20000);
  const page = await context.newPage();
  await page.goto(endpoints.product);
  await recording?.show(page);
  await hold(1400);
  await page
    .getByRole("button", { name: "Start application", exact: false })
    .click();
  const popup = context.waitForEvent("page", { timeout: 15000 });
  await page.getByRole("link", { name: "Review sharing request" }).click();
  const portal = await popup;
  const registry = portal.getByRole("button", {
    name: "Verify registry membership",
    exact: true,
  });
  await signIn(portal, registry);
  await recording?.show(portal);
  await hold(1400);
  await registry.click();
  await portal
    .getByRole("heading", { name: "The exact notice", exact: true })
    .waitFor();
  await hold(2500);
  return { page, portal };
}

try {
  const { page, portal } = await review();
  await portal.getByRole("checkbox").check();
  await portal
    .getByRole("button", { name: "Approve this request", exact: true })
    .click();
  await portal
    .getByRole("heading", { name: "Your permission record", exact: true })
    .waitFor();
  await hold(1600);
  recording?.pause();
  await page.bringToFront();
  await recording?.show(page);
  await page.getByRole("button", { name: "Get approved details" }).click();
  await page
    .getByRole("heading", { name: "Your details are ready for review" })
    .waitFor();
  assert.equal(
    await page.locator("#membership-value").textContent(),
    "Active member",
  );
  assert.equal(
    await page.locator("#savings-value").textContent(),
    "₹12,500.75",
  );
  await hold(2400);
  await page.screenshot({
    path: join(state, "approved-result.png"),
    fullPage: true,
  });
  console.log(
    `PASS: ${hosted ? "hosted stack" : "standalone launcher"} completed official eSignet, explicit consent and exact SakhiCredit retrieval`,
  );
  if (!hosted) {
    await page.getByText("View sharing reference", { exact: true }).click();
    reportDemoLegalHold(
      endpoints.data, join(state, "certs"),
      await page.locator("#receipt-reference").textContent(),
    );
    recording?.pause();
    await portal.bringToFront();
    await showDemoLegalHold(portal);
    await recording?.show(portal);
    await hold(4000);
    console.log("PASS: authenticated consent receipt shows the recipient-reported legal hold");
  }
  const denied = await review();
  await denied.portal
    .getByRole("button", { name: "Reject this request", exact: true })
    .click();
  await denied.portal
    .getByRole("heading", { name: "Your permission record", exact: true })
    .waitFor();
  await hold(2000);
  recording?.pause();
  await denied.page.bringToFront();
  await recording?.show(denied.page);
  await denied.page
    .getByRole("heading", { name: "Sharing declined" })
    .waitFor();
  assert.equal(await denied.page.locator("#retrieve").isHidden(), true);
  assert.equal(
    await denied.page.locator("#approved-result").isHidden(),
    true,
  );
  await hold(3000);
  console.log("PASS: rejected request did not enable retrieval or show data");
  recording?.pause();
  const historyContext = await browser.newContext({
    viewport: { width: 1280, height: 800 },
  });
  const history = await historyContext.newPage();
  await history.goto(endpoints.gateway + "/consent-history/lokos");
  const historySignIn = history.getByRole("link", { name: "Sign in to view history" });
  await historySignIn.waitFor();
  assert.equal(await history.getByRole("link", { name: "Download consent record" }).count(), 0);
  await recording?.show(history);
  await hold(1800);
  await historySignIn.click();
  const records = history.getByRole("link", { name: "Download consent record" });
  await signIn(history, records.first());
  const requestID = (page) => new URL(page.url()).pathname.split("/")[2];
  const approvedID = requestID(portal);
  const deniedID = requestID(denied.portal);
  const receiptPath = (id) => `/consent-history/lokos/${id}/receipt`;
  const approvedRecord = history.locator(`a[href="${receiptPath(approvedID)}"]`);
  const deniedRecord = history.locator(`a[href="${receiptPath(deniedID)}"]`);
  assert.match(await approvedRecord.locator("..").textContent(), /Single use spent · Request COMPLETED/);
  assert.match(await deniedRecord.locator("..").textContent(), /Request REJECTED/);
  const accessAudit = approvedRecord.locator("..");
  await accessAudit.getByText("Approval used to authorize retrieval").waitFor();
  await accessAudit.getByText("Authorization event reference").waitFor();
  await accessAudit.getByText("1 of 1 authorized retrievals used.", { exact: true }).waitFor();
  await accessAudit.getByRole("heading", { name: "Access audit" }).scrollIntoViewIfNeeded();
  await recording?.show(history);
  await hold(5000);
  await deniedRecord.locator("..").scrollIntoViewIfNeeded();
  await hold(2600);
  const download = history.waitForEvent("download", { timeout: 5000 });
  await approvedRecord.click();
  const recovered = JSON.parse(readFileSync(await (await download).path(), "utf8"));
  assert.equal(recovered.request_id, approvedID);
  assert.equal(recovered.decision.decision, "approve");
  await approvedRecord.locator("..").getByRole("link", { name: "Rights and grievances" }).click();
  await history.getByRole("heading", { name: "Rights and grievances", exact: true }).waitFor();
  await history.getByLabel("Request type").selectOption("correction");
  const caseID = await history.locator('input[name="case_id"]').inputValue();
  await hold(2000);
  await history.getByRole("button", { name: "Submit case" }).click();
  const caseStatus = history.getByText("correction · queued", { exact: true });
  await caseStatus.waitFor();
  const caseDetails = caseStatus.locator("../..");
  await caseDetails.getByText("Responsible registry").waitFor();
  assert.match(await caseDetails.textContent(), new RegExp(caseID));
  await caseDetails.scrollIntoViewIfNeeded();
  await hold(2800);
  if (!hosted) {
    acknowledgeDemoCorrection(endpoints.data, join(state, "certs"), caseID);
    await history.reload();
    const acknowledged = history.getByText("correction · acknowledged", { exact: true });
    await acknowledged.waitFor();
    await acknowledged.locator("../..").scrollIntoViewIfNeeded();
    await hold(3500);
    console.log("PASS: owner opened a correction case and saw the registry operator acknowledgement after refresh");
  } else {
    console.log("PASS: hosted correction shows the assigned registry and queued state");
  }
  recording?.pause();
  await historyContext.close();
  console.log("PASS: fresh eSignet sign-in recovered approved and rejected history and downloaded the approved receipt");
  if (recording && !hosted) {
    recording.pause();
    const evidenceScript = fileURLToPath(
      new URL("../../../infra/demo/verify-evidence.py", import.meta.url),
    );
    const evidence = JSON.parse(
      execFileSync("python3", [evidenceScript, state], {
        encoding: "utf8",
        timeout: 50000,
      }),
    );
    assert.equal(evidence.mode, "offline");
    assert.ok(
      Number.isSafeInteger(evidence.verified_commitments) &&
        evidence.verified_commitments > 0,
    );
    const proof = await browser.newPage({
      viewport: { width: 1280, height: 800 },
    });
    await proof.setContent(`<!doctype html><meta charset="utf-8">
      <title>GRAMStack operator evidence</title>
      <style>
      body{margin:0;background:#f5f7f3;color:#19382e;font:24px system-ui}
      main{margin:90px auto;max-width:900px;padding:48px;background:white;border-radius:22px}
      h1{font-size:56px;line-height:1.05;margin:18px 0}
      pre{background:#e9f2e8;padding:22px;border-radius:12px;font-size:25px}
      </style>
      <main><p>GRAMStack · Local operator verification</p>
      <h1>DeDi inclusion verified offline</h1>
      <p>The read-only verifier checked the retained commitments against the pinned checkpoint key.</p>
      <pre>{ "verified_commitments": ${evidence.verified_commitments}, "mode": "offline" }</pre>
      <p>Aggregate result from this rehearsal stack. Evidence and source payloads remain private.</p></main>`);
    recording.show(proof);
    await hold(4200);
    console.log(
      `PASS: filmed aggregate verification for ${evidence.verified_commitments} retained commitments`,
    );
    await showCapabilities(proof);
    console.log("PASS: filmed the committed capability report");
  } else if (recording) {
    const report = await browser.newPage({ viewport: { width: 1280, height: 800 } });
    recording.show(report);
    await showCapabilities(report);
    console.log("PASS: filmed the hosted capability report");
  }
} finally {
  try {
    await recording?.stop();
  } finally {
    await browser.close();
  }
}
