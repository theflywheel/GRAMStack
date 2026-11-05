import { mkdir, readFile, readdir, writeFile, rm } from 'node:fs/promises'
const source = new URL('../../specs/', import.meta.url)
const target = new URL('../content/specifications/', import.meta.url)
const sourceLink = 'https://github.com/theflywheel/GRAMStack/tree/main/specs'
await rm(target, { recursive: true, force: true })
await mkdir(target, { recursive: true })
const front = title => `---\ntitle: ${title}\ndescription: Versioned GRAMStack implementation contracts and conformance requirements.\n---\n\n`
const readme = await readFile(new URL('README.md', source), 'utf8')
const index = readme.replace(/\]\(0\.3\.0\/([^.)]+)\.(md|json)\)/g, ']($1)')
await writeFile(new URL('index.md', target), front('Implementation specifications') + index + `\n[Canonical repository source](${sourceLink}).\n`)
for (const file of await readdir(new URL('0.3.0/', source))) {
  const body = await readFile(new URL(`0.3.0/${file}`, source), 'utf8')
  const base = file.replace(/\.(md|json)$/, '')
  const title = { openapi: 'HTTP API specification', schemas: 'Message schemas', lifecycle: 'Lifecycle transitions', examples: 'Contract examples' }[base] ?? body.match(/^# (.+)/m)[1]
  const download = `https://docs.gramstack.try-dough.com/static/specs/0.3.0/${file}`
  const text = file.endsWith('.md') ? body : `\`\`\`json\n${body.trim()}\n\`\`\`\n`
  await writeFile(new URL(`${base}.md`, target), front(title) + `Specification version **0.3.0** · [Specifications index](/specifications/) · [Download source](${download})\n\n` + text + `\n[Repository source](${sourceLink}/0.3.0/${file}).\n`)
}
console.log('Generated specification pages from canonical specs/ sources.')

const verificationRoot = new URL('subject-verification/0.1.0/', source)
const verificationReadme = await readFile(new URL('README.md', verificationRoot), 'utf8')
const verificationDownload = 'https://docs.gramstack.try-dough.com/static/specs/subject-verification/0.1.0/'
const verificationText = verificationReadme.replace(/\]\((schemas|examples)\.json\)/g, `](${verificationDownload}$1.json)`)
await writeFile(new URL('subject-verification.md', target), front('Registry subject verification') + verificationText + '\n[Download schemas](' + verificationDownload + 'schemas.json) · [Download fixtures](' + verificationDownload + 'examples.json)\n')

const portalRoot = new URL('consent-portal/0.1.0/', source)
const portalReadme = await readFile(new URL('README.md', portalRoot), 'utf8')
const portalDownload = 'https://docs.gramstack.try-dough.com/static/specs/consent-portal/0.1.0/'
const portalText = portalReadme.replace(/\]\((schemas|examples)\.json\)/g, `](${portalDownload}$1.json)`)
await writeFile(new URL('consent-portal.md', target), front('First-party consent portal') + portalText + '\n[Download schemas](' + portalDownload + 'schemas.json) · [Download fixtures](' + portalDownload + 'examples.json)\n')

const discoveryRoot = new URL('adapter-discovery/0.1.0/', source)
const discoveryReadme = await readFile(new URL('README.md', discoveryRoot), 'utf8')
const discoveryDownload = 'https://docs.gramstack.try-dough.com/static/specs/adapter-discovery/0.1.0/'
await writeFile(new URL('adapter-discovery.md', target), front('Adapter discovery and public keys') + discoveryReadme.replace(/\]\((schemas|examples)\.json\)/g, `](${discoveryDownload}$1.json)`) + '\n[Download schemas](' + discoveryDownload + 'schemas.json) · [Download fixtures](' + discoveryDownload + 'examples.json)\n')

const postDisclosure = await readFile(new URL('post-disclosure/0.1.0/README.md', source), 'utf8')
const postDisclosureDownload = 'https://docs.gramstack.try-dough.com/static/specs/post-disclosure/0.1.0/README.md'
await writeFile(new URL('post-disclosure.md', target), front('Post-disclosure profile') + postDisclosure + '\n[Download canonical profile](' + postDisclosureDownload + ')\n')

const runtimeReadme = await readFile(new URL('runtime/0.1.0/README.md', source), 'utf8')
await writeFile(new URL('runtime.md', target), front('Implemented API catalogue') + runtimeReadme)
