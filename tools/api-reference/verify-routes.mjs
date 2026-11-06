import assert from "node:assert/strict"
import { readFile } from "node:fs/promises"

// Compare the business route registrations with the implemented catalogue.
const runtime = new Set()
for (const name of ["gateway", "gateway-private", "adapter", "sakhicredit"]) {
  const api = JSON.parse(await readFile(new URL("../../specs/runtime/0.2.0/" + name + ".json", import.meta.url), "utf8"))
  for (const [path, methods] of Object.entries(api.paths)) {
    for (const method of Object.keys(methods)) runtime.add(method.toUpperCase() + " " + path)
  }
}
const routeFiles = [
  "services/gateway/internal/consumer/http.go",
  "services/gateway/internal/authorization/http.go",
  "services/gateway/internal/retrieval/http.go",
  "services/gateway/internal/rights/operator.go",
  "services/gateway/internal/authn/service.go",
  "services/gateway/internal/consentportal/portal.go",
  "services/gateway/cmd/gramstack/consumer.go",
  "services/gateway/cmd/gramstack/authorization.go",
  "adapters/lokos/cmd/lokos-adapter/main.go",
  "adapters/lokos/cmd/lokos-adapter/retrieval.go",
  "apps/sakhicredit/internal/application/http.go",
  "apps/sakhicredit/internal/application/login.go",
]
for (const filename of routeFiles) {
  const code = await readFile(new URL("../../" + filename, import.meta.url), "utf8")
  for (const match of code.matchAll(/\.Handle(?:Func)?\("(GET|POST|PUT|DELETE|PATCH) ([^"]+)"/g)) {
    const [, method, path] = match
    if (path.includes("/assets/") || ["/healthz", "/readyz", "/{$}"].includes(path) || path.endsWith("/")) continue
    assert.ok(runtime.has(method + " " + path), `Implemented route missing: ${filename} ${method} ${path}`)
  }
}
for (const action of ["refresh", "retrieve", "cancel"]) {
  assert.ok(runtime.has(`POST /api/applications/{id}/${action}`))
}
assert.ok(runtime.has("POST /internal/v1/subject-verifications"))
assert.ok(!runtime.has("POST /internal/consent-requests"), "In-process consent management has no deployed RPC")
console.log("Verified implemented business route coverage.")
