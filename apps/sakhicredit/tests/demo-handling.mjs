import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { join } from "node:path";

function demoPrivateRequest(dataOrigin, certs, identity, path, body) {
  const endpoint = new URL(dataOrigin);
  assert.equal(endpoint.protocol, "https:");
  assert.equal(endpoint.hostname, "127.0.0.1");
  assert.equal(endpoint.origin, dataOrigin);
  const host = "gateway.gramstack.internal";
  const request = body === undefined ? [] : [
    "--request", "PUT", "--header", "Content-Type: application/json",
    "--data-binary", JSON.stringify(body),
  ];
  return JSON.parse(execFileSync("curl", [
    "--disable", "--fail", "--silent", "--show-error", "--max-time", "5",
    "--noproxy", "*", "--resolve", `${host}:${endpoint.port}:127.0.0.1`,
    "--cacert", join(certs, "ca.pem"),
    "--cert", join(certs, `${identity}.pem`),
    "--key", join(certs, `${identity}.key`),
    ...request,
    `https://${host}:${endpoint.port}${path}`,
  ], { encoding: "utf8", timeout: 7000 }));
}

export function reportDemoLegalHold(dataOrigin, certs, receiptID) {
  assert.match(receiptID, /^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$/);
  const report = demoPrivateRequest(
    dataOrigin, certs, "consumer-one",
    `/v1/disclosures/${encodeURIComponent(receiptID)}/handling-reports/demo-legal-hold`,
    { action: "retention_exception", exception_code: "legal_hold", occurred_at: new Date().toISOString() },
  );
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

export function acknowledgeDemoCorrection(dataOrigin, certs, caseID) {
  assert.match(caseID, /^[A-Z2-7]{26}$/);
  const route = "/internal/v1/rights-cases/registry/lokos";
  const queue = demoPrivateRequest(dataOrigin, certs, "lokos-rights", route);
  assert.ok(queue.some((item) =>
    item.case_id === caseID && item.case_type === "correction" && item.status === "queued",
  ));
  const acknowledgement = demoPrivateRequest(
    dataOrigin, certs, "lokos-rights", `${route}/${caseID}/demo-acknowledge`,
    { action: "acknowledge" },
  );
  assert.equal(acknowledgement.status, "acknowledged");
  assert.equal(acknowledgement.status_version, 2);
}
