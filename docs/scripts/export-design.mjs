import { mkdir, writeFile, cp } from 'node:fs/promises'
import { readFileSync, readdirSync } from 'node:fs'
const chapters = readdirSync('content').filter(n => /^\d\d-.*\.md$/.test(n)).sort()
const body = chapters.map(n => readFileSync(`content/${n}`, 'utf8').replace(/^---\n[\s\S]*?\n---\n/, '')).join('\n\n---\n\n')
await mkdir('public/static', { recursive: true })
await writeFile('public/static/GRAMStack-Design.md', '# GRAMStack — System Design\n\nVersion 0.13 · 1 November 2026 · System architecture\n\n' + body)
await writeFile('public/static/gramstack-design.txt', '# GRAMStack — System Design\n\nVersion 0.13 · 1 November 2026 · System architecture\n\n' + body)
await writeFile('public/robots.txt', 'User-agent: *\nDisallow: /\n')
console.log(`Exported ${chapters.length} chapters to static/GRAMStack-Design.md`)

await cp('../specs', 'public/static/specs', { recursive: true })
console.log('Published canonical specification downloads.')
