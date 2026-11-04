import assert from 'node:assert/strict'
import { existsSync } from 'node:fs'
import { createRailwayContext, project, RAILWAY_GRAPH_VERSION, validateGraph } from 'railway/iac'
import configuration from './railway.ts'

const definition = await configuration(createRailwayContext({ environment: 'production' }), project)
const graph = { version: RAILWAY_GRAPH_VERSION, project: { name: definition.name }, environments: [], resources: definition.resources.flat(), edges: [] }
assert.deepEqual(validateGraph(graph), [])
assert.equal(definition.variables.managed, false, 'An infra apply must preserve Railway-managed variables')
for (const filename of ['railway.toml', 'railway.json']) {
  assert.ok(!existsSync(new URL('../../' + filename, import.meta.url)), 'No service-specific defaults at repository root')
}
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
assert.equal(platformDatabase.name, 'platform-db')
for (const key of ['serviceDomains', 'customDomains', 'tcpProxies'])
  assert.deepEqual(Object.keys(platformDatabase.networking?.[key] ?? {}), [], 'Database stays private')
assert.equal(platformDatabase.volumeAttachments['platform-data']?.mountPath,
  '/var/lib/postgresql/data', 'Keep the persistent PostgreSQL mount')

const hydraAdmin = graph.resources.find(r => r.address === 'service.hydra-admin')
const hydraPublic = graph.resources.find(r => r.address === 'service.hydra-public')
assert.equal(hydraAdmin.kind, 'docker-image')
assert.deepEqual(hydraAdmin.source, hydraPublic.source)
assert.match(hydraAdmin.source.image, /^oryd\/hydra:v26\.2\.0@sha256:[a-f0-9]{64}$/)
assert.deepEqual(hydraAdmin.deploy.preDeployCommand, ['hydra migrate sql up -e -y'])
assert.equal(hydraAdmin.deploy.startCommand, 'hydra serve admin --sqa-opt-out')
assert.equal(hydraPublic.deploy.startCommand, 'hydra serve public --sqa-opt-out')
for (const key of ['serviceDomains', 'customDomains', 'tcpProxies'])
  assert.deepEqual(Object.keys(hydraAdmin.networking?.[key] ?? {}), [], 'Hydra admin stays private')
assert.equal(hydraPublic.networking.serviceDomains['gramstack-hydra-b9b69819.up.railway.app'].port, 4444)

const dedi = graph.resources.find(r => r.address === 'service.dedi')
assert.equal(dedi.build.dockerfilePath, 'infra/railway/dedi/Dockerfile')
assert.equal(dedi.deploy.healthcheckPath, '/healthz')
for (const key of ['serviceDomains', 'customDomains', 'tcpProxies'])
  assert.deepEqual(Object.keys(dedi.networking?.[key] ?? {}), [], 'DeDi stays private')

const identityMounts = {
  'esignet-db': ['esignet-db-data', '/var/lib/postgresql/data'],
  'esignet-redis': ['esignet-redis-data', '/data'],
  'esignet-mock': ['esignet-mock-keys', '/keys'],
  esignet: ['esignet-keys', '/opt/mosip/keys'],
}
for (const [name, [volume, path]] of Object.entries(identityMounts)) {
  const resource = graph.resources.find(r => r.address === `service.${name}`)
  assert.equal(resource.volumeAttachments[volume]?.mountPath, path)
  for (const key of ['serviceDomains', 'customDomains', 'tcpProxies'])
    assert.deepEqual(Object.keys(resource.networking?.[key] ?? {}), [], `${name} stays private`)
}
const esignetUI = graph.resources.find(r => r.address === 'service.esignet-ui')
assert.equal(esignetUI.deploy.healthcheckPath, '/healthz')
assert.deepEqual(esignetUI.networking.serviceDomains,
  { 'gramstack-esignet-b9b69819.up.railway.app': { port: 3000 } })

for (const [name, host] of [
  ['gateway', 'gramstack-gateway-b9b69819.up.railway.app'],
  ['lokos-adapter', 'gramstack-adapter-b9b69819.up.railway.app'],
  ['mock-lokos', undefined],
  ['sakhicredit', 'sakhicredit-b9b69819.up.railway.app'],
]) {
  const resource = graph.resources.find(r => r.address === `service.${name}`)
  assert.equal(resource.deploy.healthcheckPath, host ? '/readyz' : undefined)
  assert.deepEqual(resource.deploy.preDeployCommand, host ? ['/app/runtimefiles /app/service migrate'] : undefined)
  assert.deepEqual(resource.networking?.serviceDomains ?? {}, host ? { [host]: { port: 8080 } } : {})
  assert.deepEqual(Object.keys(resource.networking?.tcpProxies ?? {}), [])
  assert.deepEqual(Object.keys(resource.networking?.customDomains ?? {}), [])
}
