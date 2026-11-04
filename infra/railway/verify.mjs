import assert from 'node:assert/strict'
import { existsSync } from 'node:fs'
import { createRailwayContext, project, RAILWAY_GRAPH_VERSION, validateGraph } from 'railway/iac'
import configuration from './railway.ts'

const definition = await configuration(createRailwayContext({ environment: 'production' }), project)
const graph = { version: RAILWAY_GRAPH_VERSION, project: { name: definition.name }, environments: [], resources: definition.resources.flat(), edges: [] }
assert.deepEqual(validateGraph(graph), [])
assert.equal(definition.variables.managed, false, 'An infra apply must preserve Railway-managed variables')
assert.ok(!existsSync(new URL('../../railway.toml', import.meta.url)), 'No service-specific defaults at repository root')
for (const resource of graph.resources.filter(r => r.type === 'service')) {
  if (resource.kind === 'docker-image') {
    assert.ok(resource.source.image, `${resource.name}: image reference required`)
    assert.ok(!resource.build?.dockerfilePath, `${resource.name}: image services must not inherit a Dockerfile`)
  }
  if (resource.build?.dockerfilePath) {
    assert.ok(existsSync(new URL('../../' + resource.build.dockerfilePath, import.meta.url)), `${resource.name}: Dockerfile missing`)
  }
}
const docs = graph.resources.find(r => r.address === 'service.docs')
assert.ok(docs, 'Keep the existing docs service in the project graph')
assert.equal(docs.build.dockerfilePath, 'docs/Dockerfile')
assert.equal(docs.deploy.healthcheckPath, '/healthz')
assert.equal(docs.networking.customDomains['docs.gramstack.try-dough.com'].port, 8080)
console.log(`Verified Railway graph with ${graph.resources.length} resource(s), service-specific builds and preserved docs routing.`)

const platformDatabase = graph.resources.find(r => r.address === 'service.platform-db')
assert.ok(platformDatabase, 'Keep the platform database in the project graph')
assert.equal(platformDatabase.build.dockerfilePath, 'infra/railway/database/Dockerfile')
assert.equal(platformDatabase.networking.privateNetworkEndpoint, 'platform-db')
for (const key of ['serviceDomains', 'customDomains', 'tcpProxies'])
  assert.deepEqual(Object.keys(platformDatabase.networking[key] ?? {}), [], 'Database stays private')
assert.equal(platformDatabase.volumeAttachments['platform-data']?.mountPath,
  '/var/lib/postgresql/data', 'Keep the persistent PostgreSQL mount')
