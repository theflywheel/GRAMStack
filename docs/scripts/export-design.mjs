import { mkdir, writeFile, cp, stat } from 'node:fs/promises'
import { readFileSync, readdirSync } from 'node:fs'
const chapters = readdirSync('content/architecture').filter(n => /^\d\d-.*\.md$/.test(n)).sort()
const body = chapters.map(n => readFileSync(`content/architecture/${n}`, 'utf8').replace(/^---\n[\s\S]*?\n---\n/, '')).join('\n\n---\n\n')
await mkdir('public/static', { recursive: true })
// Older share previews may still request the original image URL.
await cp('public/static/gramstack-share-v1.png', 'public/static/og-image.png')
await writeFile('public/static/GRAMStack-Design.md', '# GRAMStack — System Design\n\nVersion 0.15 · 4 November 2026 · System architecture\n\n' + body)
await writeFile('public/static/gramstack-design.txt', '# GRAMStack — System Design\n\nVersion 0.15 · 4 November 2026 · System architecture\n\n' + body)
await writeFile('public/robots.txt', 'User-agent: *\nDisallow: /\n')
console.log(`Exported ${chapters.length} chapters to static/GRAMStack-Design.md`)

await cp('../specs', 'public/static/specs', { recursive: true })
console.log('Published canonical specification downloads.')


// Embed the same markup used by the full-window walkthrough in the Quartz article.
const source = readFileSync('quartz/static/architecture-walkthrough.html', 'utf8')
const walkthrough = /<!-- walkthrough:start -->([\s\S]+?)<!-- walkthrough:end -->/.exec(source)?.[1].trim()
if (!walkthrough) throw new Error('Walkthrough markup missing from the static source')
for (const slug of ['architecture/02-architecture', 'architecture/complete-design']) {
  const path = `public/${slug}.html`
  const html = readFileSync(path, 'utf8')
  const placeholder = /<div data-architecture-walkthrough(?:="")?><\/div>/
  if (!placeholder.test(html)) throw new Error(`Walkthrough placeholder missing from ${slug}`)
  await writeFile(path, html.replace(placeholder, walkthrough))
}

// Legacy routes work on the Railway server and static-only Quartz previews.
const redirects = JSON.parse(readFileSync('redirects.json', 'utf8'))
await writeFile('public/static/redirects.json', JSON.stringify(redirects))
for (const [old, target] of Object.entries(redirects)) {
  const page = 'public' + target
  const candidates = [page + '.html', page + '/index.html']
  if (!(await Promise.all(candidates.map(file => stat(file).then(s => s.isFile()).catch(() => false)))).some(Boolean)) throw new Error('Missing redirect target: ' + target)
  const html = '<!doctype html><html lang="en"><head><meta charset="utf-8"><title>Page moved · GRAMStack</title><link rel="canonical" href="' + target + '"><meta name="robots" content="noindex"><script>location.replace(' + JSON.stringify(target) + ' + location.search + location.hash)</script><meta http-equiv="refresh" content="0; url=' + target + '"></head><body><a href="' + target + '">Continue to this page</a></body></html>'
  await mkdir(new URL('.', new URL('public' + old + '.html', 'file://' + process.cwd() + '/')), { recursive: true })
  await writeFile('public' + old + '.html', html)
  if (old === '/specifications') {
    await mkdir('public/specifications', { recursive: true })
    await writeFile('public/specifications/index.html', html)
  }
}
console.log(`Preserved ${Object.keys(redirects).length} legacy page URLs, including query strings and fragments.`)
