import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { chromium } from "playwright-core";
import { setTimeout as delay } from "node:timers/promises";
import { captureDemo } from "./demo-capture.mjs";
const state = process.argv[2];
assert.ok(
  state && process.env.CHROME_BIN,
  "Pass the demo state directory and set CHROME_BIN",
);
const endpoints = JSON.parse(
  readFileSync(join(state, "endpoints.json"), "utf8"),
);
assert.equal(new URL(endpoints.product).origin, endpoints.product);
assert.equal(new URL(endpoints.product).hostname, "127.0.0.1");
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
    "PASS: standalone launcher completed official eSignet, explicit consent and exact SakhiCredit retrieval",
  );
  if (recording) {
    const denied = await review();
    await denied.portal
      .getByRole("button", { name: "Reject this request", exact: true })
      .click();
    await denied.portal
      .getByRole("heading", { name: "Your permission record", exact: true })
      .waitFor();
    await hold(2000);
    recording.pause();
    await denied.page.bringToFront();
    await recording.show(denied.page);
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
  }
} finally {
  try {
    await recording?.stop();
  } finally {
    await browser.close();
  }
}
