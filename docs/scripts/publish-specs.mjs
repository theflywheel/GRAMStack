import { mkdir, readFile, readdir, writeFile } from 'node:fs/promises'
const source = new URL('../../specs/', import.meta.url)
const target = new URL('../content/developers/specifications/', import.meta.url)
const redirects = JSON.parse(await readFile(new URL('../redirects.json', import.meta.url), 'utf8'))
const content = new URL('../content/', import.meta.url)
const canonical = text => text.replace(/\]\(([^)\s]+)\)/g, (link, href) => {
  const [path, fragment] = href.split('#')
  const local = path.replace('https://docs.gramstack.try-dough.com', '').replace(/\/$/, '')
  const key = local.startsWith('/specifications') ? local : '/specifications/' + local.replace(/\.(md|json)$/, '')
  const target = redirects[key] ?? (local === '/sdks' ? '/developers/sdks' : undefined)
  return target ? '](' + target + (fragment ? '#' + fragment : '') + ')' : link
})
async function publish(name, body) {
  const old = name === 'index.md' ? '/specifications' : '/specifications/' + name.replace(/\.md$/, '')
  const slug = redirects[old]
  if (!slug) throw new Error('Missing canonical specification path: ' + old)
  const destination = new URL(slug.slice(1) + (name === 'index.md' ? '/index.md' : '.md'), content)
  await mkdir(new URL('.', destination), { recursive: true })
  await writeFile(destination, canonical(body))
}
const sourceLink = 'https://github.com/theflywheel/GRAMStack/tree/main/specs'
await mkdir(target, { recursive: true })
const front = title => `---\ntitle: ${title}\ndescription: Versioned GRAMStack implementation contracts and conformance requirements.\n---\n\n`
const readme = await readFile(new URL('README.md', source), 'utf8')
const index = readme.replace(/\]\(0\.3\.0\/([^.)]+)\.(md|json)\)/g, (_, name) => '](' + redirects['/specifications/' + name] + ')')
await publish('index.md', front('Specifications') + 'Choose a contract family: [Core protocol](/developers/specifications/core) · [Authentication and consent profiles](/developers/specifications/profiles) · [Adapter contracts](/developers/specifications/adapters) · [Schemas and examples](/developers/specifications/schemas) · [Conformance and versioning](/developers/specifications/conformance)\n\n' + index + `\n[Canonical repository source](${sourceLink}).\n`)
for (const file of await readdir(new URL('0.3.0/', source))) {
  const body = await readFile(new URL(`0.3.0/${file}`, source), 'utf8')
  const base = file.replace(/\.(md|json)$/, '')
  const title = { openapi: 'HTTP API specification', schemas: 'Message schemas', lifecycle: 'Lifecycle transitions', examples: 'Contract examples' }[base] ?? body.match(/^# (.+)/m)[1]
  const download = `https://docs.gramstack.try-dough.com/static/specs/0.3.0/${file}`
  const text = file.endsWith('.md') ? body : `\`\`\`json\n${body.trim()}\n\`\`\`\n`
  await publish(`${base}.md`, front(title) + `Specification version **0.3.0** · [Specifications index](/specifications/) · [Download source](${download})\n\n` + text + `\n[Repository source](${sourceLink}/0.3.0/${file}).\n`)
}
console.log('Generated specification pages from canonical specs/ sources.')

const verificationRoot = new URL('subject-verification/0.1.0/', source)
const verificationReadme = await readFile(new URL('README.md', verificationRoot), 'utf8')
const verificationDownload = 'https://docs.gramstack.try-dough.com/static/specs/subject-verification/0.1.0/'
const verificationText = verificationReadme.replace(/\]\((schemas|examples)\.json\)/g, `](${verificationDownload}$1.json)`)
await publish('subject-verification.md', front('Registry subject verification') + verificationText + '\n[Download schemas](' + verificationDownload + 'schemas.json) · [Download fixtures](' + verificationDownload + 'examples.json)\n')

const portalRoot = new URL('consent-portal/0.1.0/', source)
const portalReadme = await readFile(new URL('README.md', portalRoot), 'utf8')
const portalDownload = 'https://docs.gramstack.try-dough.com/static/specs/consent-portal/0.1.0/'
const portalText = portalReadme.replace(/\]\((schemas|examples)\.json\)/g, `](${portalDownload}$1.json)`)
await publish('consent-portal.md', front('First-party consent portal') + portalText + '\n[Download schemas](' + portalDownload + 'schemas.json) · [Download fixtures](' + portalDownload + 'examples.json)\n')

const discoveryRoot = new URL('adapter-discovery/0.1.0/', source)
const discoveryReadme = await readFile(new URL('README.md', discoveryRoot), 'utf8')
const discoveryDownload = 'https://docs.gramstack.try-dough.com/static/specs/adapter-discovery/0.1.0/'
await publish('adapter-discovery.md', front('Adapter discovery and public keys') + discoveryReadme.replace(/\]\((schemas|examples)\.json\)/g, `](${discoveryDownload}$1.json)`) + '\n[Download schemas](' + discoveryDownload + 'schemas.json) · [Download fixtures](' + discoveryDownload + 'examples.json)\n')

const postDisclosure = await readFile(new URL('post-disclosure/0.1.0/README.md', source), 'utf8')
const postDisclosureDownload = 'https://docs.gramstack.try-dough.com/static/specs/post-disclosure/0.1.0/README.md'
await publish('post-disclosure.md', front('Post-disclosure profile') + postDisclosure + '\n[Download canonical profile](' + postDisclosureDownload + ')\n')

const runtimeReadme = await readFile(new URL('runtime/0.2.0/README.md', source), 'utf8')
await publish('runtime.md', front('Implemented API catalogue') + runtimeReadme)

const appAuthReadme = await readFile(new URL('application-auth/0.1.0/README.md', source), 'utf8')
await publish('application-auth.md', front('Application authentication') + appAuthReadme)
