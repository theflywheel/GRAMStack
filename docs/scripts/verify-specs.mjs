import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import Ajv2020 from 'ajv/dist/2020.js'
import addFormats from 'ajv-formats'
import SwaggerParser from '@apidevtools/swagger-parser'
const root = new URL('../../specs/0.1.0/', import.meta.url)
const read = name => JSON.parse(readFileSync(new URL(name, root), 'utf8'))
const schemas = read('schemas.json')
const ajv = new Ajv2020({ allErrors: true, strict: true })
addFormats(ajv)
ajv.addSchema(schemas)
for (const name of Object.keys(schemas.$defs)) ajv.getSchema(`${schemas.$id}#/$defs/${name}`)
const examples = read('examples.json').cases
for (const example of examples) {
  const validate = ajv.getSchema(`${schemas.$id}#/$defs/${example.schema}`)
  assert.ok(validate, `Unknown fixture schema: ${example.schema}`)
  assert.equal(validate(example.value), example.valid, `${example.name}: ${ajv.errorsText(validate.errors)}`)
}
for (const name of Object.keys(schemas.$defs)) {
  assert.ok(examples.some(e => e.schema === name && e.valid), `Missing positive example for ${name}`)
}
const api = await SwaggerParser.validate(new URL('openapi.json', root).pathname, { resolve: { http: false } })
const operationIds = new Set()
for (const [path, methods] of Object.entries(api.paths)) for (const [method, op] of Object.entries(methods)) {
  assert.ok(!operationIds.has(op.operationId), `Duplicate operationId ${op.operationId}`)
  operationIds.add(op.operationId)
  assert.ok(op.security, `${path}: explicit security required`)
  if (path !== '/.well-known/gramstack-adapter') assert.ok(op.security.length, `${path}: missing authentication`)
  for (const schema of Object.values(op.requestBody?.content ?? {}).map(c => c.schema)) ajv.compile(schema)
  for (const response of Object.values(op.responses)) for (const media of Object.values(response.content ?? {})) ajv.compile(media.schema)
  for (const param of op.parameters ?? []) {
    if (param.in === 'path') assert.ok(path.includes(`{${param.name}}`) && param.required)
  }
  assert.ok(method === 'get' || method === 'post')
}
const lifecycle = read('lifecycle.json')
const states = Object.keys(lifecycle.transitions)
assert.deepEqual(new Set(states), new Set(schemas.$defs.RequestStatus.properties.status.enum))
const reachable = new Set([lifecycle.initial])
for (let pass = 0; pass < states.length; pass++) for (const state of [...reachable]) {
  for (const next of lifecycle.transitions[state]) { assert.ok(states.includes(next)); reachable.add(next) }
}
assert.equal(reachable.size, states.length, 'Unreachable lifecycle state')
assert.deepEqual(new Set(lifecycle.terminal), new Set(states.filter(s => !lifecycle.transitions[s].length)))
assert.ok(!lifecycle.transitions.RELEASE_ATTEMPTED.includes('FAILED'), 'Ambiguous release must be DELIVERY_UNKNOWN')
assert.equal(schemas.$defs.RegistryPolicy.properties.max_disclosures.const, 1)
assert.equal(schemas.$defs.RegistryPolicy.properties.require_human_consent.const, true)
const grant = examples.find(e => e.schema === 'RetrievalGrant' && e.valid).value
assert.ok(grant.exp > grant.iat && grant.exp - grant.iat <= 300)
assert.equal(grant.cnf['x5t#S256'].length, 43)
const dataSecurity = api.paths['/v1/access-requests/{id}/data'].get.security
assert.ok(dataSecurity.some(s => 'retrievalGrant' in s && 'presenterMTLS' in s))
console.log(`Verified ${operationIds.size} HTTP operations, ${Object.keys(schemas.$defs).length} message schemas, ${examples.length} fixtures and ${states.length} lifecycle states.`)
