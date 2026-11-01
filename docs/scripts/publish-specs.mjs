import { mkdir, readFile, readdir, writeFile, rm } from 'node:fs/promises'
const source = new URL('../../specs/', import.meta.url)
const target = new URL('../content/specifications/', import.meta.url)
const sourceLink = 'https://github.com/theflywheel/GRAMStack/tree/main/specs'
await rm(target, { recursive: true, force: true })
await mkdir(target, { recursive: true })
const front = title => `---\ntitle: ${title}\ndescription: Versioned GRAMStack implementation contracts and conformance requirements.\n---\n\n`
const readme = await readFile(new URL('README.md', source), 'utf8')
const index = readme.replace(/\]\(0\.1\.0\/([^.)]+)\.(md|json)\)/g, ']($1)')
await writeFile(new URL('index.md', target), front('Implementation specifications') + index + `\n[Canonical repository source](${sourceLink}).\n`)
for (const file of await readdir(new URL('0.1.0/', source))) {
  const body = await readFile(new URL(`0.1.0/${file}`, source), 'utf8')
  const base = file.replace(/\.(md|json)$/, '')
  const title = { openapi: 'HTTP API specification', schemas: 'Message schemas', lifecycle: 'Lifecycle transitions', examples: 'Contract examples' }[base] ?? body.match(/^# (.+)/m)[1]
  const download = `https://docs.gramstack.try-dough.com/static/specs/0.1.0/${file}`
  const text = file.endsWith('.md') ? body : `\`\`\`json\n${body.trim()}\n\`\`\`\n`
  await writeFile(new URL(`${base}.md`, target), front(title) + `Specification version **0.1.0** · [Specifications index](/specifications/) · [Download source](${download})\n\n` + text + `\n[Repository source](${sourceLink}/0.1.0/${file}).\n`)
}
console.log('Generated specification pages from canonical specs/ sources.')
