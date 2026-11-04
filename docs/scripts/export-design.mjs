import { mkdir, writeFile, cp } from 'node:fs/promises'
import { readFileSync, readdirSync } from 'node:fs'
const chapters = readdirSync('content').filter(n => /^\d\d-.*\.md$/.test(n)).sort()
const body = chapters.map(n => readFileSync(`content/${n}`, 'utf8').replace(/^---\n[\s\S]*?\n---\n/, '')).join('\n\n---\n\n')
await mkdir('public/static', { recursive: true })
await writeFile('public/static/GRAMStack-Design.md', '# GRAMStack — System Design\n\nVersion 0.14 · 3 November 2026 · System architecture\n\n' + body)
await writeFile('public/static/gramstack-design.txt', '# GRAMStack — System Design\n\nVersion 0.14 · 3 November 2026 · System architecture\n\n' + body)
await writeFile('public/robots.txt', 'User-agent: *\nDisallow: /\n')
console.log(`Exported ${chapters.length} chapters to static/GRAMStack-Design.md`)

await cp('../specs', 'public/static/specs', { recursive: true })
console.log('Published canonical specification downloads.')


// Embed the same markup used by the full-window walkthrough in the Quartz article.
const source = readFileSync('quartz/static/architecture-walkthrough.html', 'utf8')
const walkthrough = /<!-- walkthrough:start -->([\s\S]+?)<!-- walkthrough:end -->/.exec(source)?.[1].trim()
if (!walkthrough) throw new Error('Walkthrough markup missing from the static source')
for (const slug of ['02-architecture', 'complete-design']) {
  const path = `public/${slug}.html`
  const html = readFileSync(path, 'utf8')
  const placeholder = /<div data-architecture-walkthrough(?:="")?><\/div>/
  if (!placeholder.test(html)) throw new Error(`Walkthrough placeholder missing from ${slug}`)
  await writeFile(path, html.replace(placeholder, walkthrough))
}
