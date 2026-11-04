import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { join } from "node:path";

export function reportDemoLegalHold(dataOrigin, certs, receiptID) {
  const endpoint = new URL(dataOrigin);
  assert.equal(endpoint.protocol, "https:");
  assert.equal(endpoint.hostname, "127.0.0.1");
  assert.equal(endpoint.origin, dataOrigin);
  assert.match(receiptID, /^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$/);
  const host = "gateway.gramstack.internal";
  const response = execFileSync("curl", [
    "--disable", "--fail", "--silent", "--show-error", "--max-time", "5",
    "--noproxy", "*", "--resolve", `${host}:${endpoint.port}:127.0.0.1`,
    "--cacert", join(certs, "ca.pem"),
    "--cert", join(certs, "consumer-one.pem"),
    "--key", join(certs, "consumer-one.key"),
    "--request", "PUT", "--header", "Content-Type: application/json",
    "--data-binary", JSON.stringify({
      action: "retention_exception",
      exception_code: "legal_hold",
      occurred_at: new Date().toISOString(),
    }),
    `https://${host}:${endpoint.port}/v1/disclosures/${encodeURIComponent(receiptID)}/handling-reports/demo-legal-hold`,
  ], { encoding: "utf8", timeout: 7000 });
  const report = JSON.parse(response);
  assert.equal(report.receipt_id, receiptID);
  assert.equal(report.exception_code, "legal_hold");
  assert.equal(report.verification, "reported");
}

export async function showDemoLegalHold(page) {
  await page.reload();
  const report = page.getByRole("region", { name: "Recipient handling reports" });
  await report.getByText(/Legal hold/).waitFor();
  await report.getByText(/has not independently verified/).waitFor();
  await report.scrollIntoViewIfNeeded();
}
