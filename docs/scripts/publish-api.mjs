import assert from "node:assert/strict"
import { mkdir, readFile, writeFile, copyFile } from "node:fs/promises"
import SwaggerParser from "@apidevtools/swagger-parser"
import swaggerUI from "swagger-ui-dist"

const source = new URL("../../specs/", import.meta.url)
const destination = new URL("../public/api/", import.meta.url)
await mkdir(destination, { recursive: true })
const options = {
  resolve: {
    http: false,
    canonical: {
      order: 1,
      canRead: /^https:\/\/docs\.gramstack\.try-dough\.com\/static\/specs\//,
      read: file => readFile(new URL(new URL(file.url).pathname.slice("/static/specs/".length), source), "utf8"),
    },
  },
}
const catalogues = [
  ["gateway", "Gateway · public and browser"],
  ["gateway-private", "Gateway · private mTLS"],
  ["adapter", "LokOS adapter"],
  ["sakhicredit", "SakhiCredit product"],
  ["protocol", "Protocol · core 0.3.0"],
]
for (const [name] of catalogues) {
  const path = name === "protocol" ? "0.3.0/openapi.json" : `runtime/0.1.0/${name}.json`
  const file = new URL(path, source).pathname
  await SwaggerParser.validate(file, options)
  const api = await SwaggerParser.bundle(file, options)
  const ids = new Set()
  for (const [path, methods] of Object.entries(api.paths)) {
    for (const [method, operation] of Object.entries(methods)) {
      assert.ok(!ids.has(operation.operationId), `Duplicate operation ID in ${name}`)
      ids.add(operation.operationId)
      assert.ok(Array.isArray(operation.security), `${name} ${method} ${path}: explicit security required`)
    }
  }
  await writeFile(new URL(name + ".json", destination), JSON.stringify(api, null, 2) + "\n")
  console.log(`Published ${name}: ${ids.size} validated operations`)
}
for (const asset of ["swagger-ui.css", "swagger-ui-bundle.js", "swagger-ui-standalone-preset.js"]) {
  await copyFile(swaggerUI.getAbsoluteFSPath() + "/" + asset, new URL(asset, destination))
}
for (const asset of ["LICENSE", "NOTICE"]) {
  await copyFile(swaggerUI.getAbsoluteFSPath() + "/" + asset, new URL("swagger-ui-" + asset + ".txt", destination))
}
for (const asset of ["index.html", "explorer.css", "explorer.js"]) {
  await copyFile(new URL("../api/" + asset, import.meta.url), new URL(asset, destination))
}
