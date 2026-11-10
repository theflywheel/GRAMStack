import assert from 'node:assert/strict'
import { JSDOM } from 'jsdom'
import sharp from 'sharp'
import { readFileSync, existsSync, readdirSync } from 'node:fs'
import { resolve, dirname } from 'node:path'
const walk = d => readdirSync(d, { withFileTypes: true }).flatMap(e => e.isDirectory() ? walk(`${d}/${e.name}`) : [`${d}/${e.name}`])
const sources = walk('content').filter(f => f.endsWith('.md'))
const chapters = sources.filter(f => /\/\d\d-/.test(f))
assert.equal(chapters.length, 12)
let diagrams=0, json=0, links=0
for (const file of sources) {
 const text=readFileSync(file,'utf8')
 assert.match(text,/^---\ntitle:/,file)
 assert.doesNotMatch(text,/\/Users\/|\/private\/tmp\/|turquoise-joyful|hotmail\.com|LLD|sign.off|parked|ChatGPT|Claude|WhatsApp|team review|pending agreement|unconfirmed|GRAMStack login app/i,file)
 for(const m of text.matchAll(/```json\n([\s\S]*?)\n```/g)){JSON.parse(m[1]);json++}
 diagrams += [...text.matchAll(/```mermaid\n/g)].length
}
for(const file of walk('public').filter(f=>f.endsWith('.html'))){
 const text=readFileSync(file,'utf8')
 assert.doesNotMatch(text,/<title>Untitled/,file)
 for(const m of text.matchAll(/(?:href|src)="([^"]+)"/g)){
   const target=m[1].replaceAll('&amp;','&');if(/^(?:https?:|mailto:|data:|#|tel:)/.test(target))continue
   const pathname=decodeURIComponent(target.split(/[?#]/)[0]);if(!pathname)continue
   const base=pathname.startsWith('/')?resolve('public','.'+pathname):resolve(dirname(file),pathname)
   assert.ok([base,base+'.html',resolve(base,'index.html')].some(existsSync),`${file}: broken link ${target}`);links++
 }
}
assert.ok(readFileSync('public/static/contentIndex.json','utf8').includes('Consent manager'))
assert.ok(readFileSync('public/static/GRAMStack-Design.md','utf8').length>45000)
assert.ok(!existsSync('public/reference'), 'Internal historical pages must not be published')
assert.ok(!existsSync('public/api'), 'Swagger must be served by the API services')
assert.doesNotMatch(readFileSync('public/static/contentIndex.json','utf8'), /Historical baseline|LLD|sign.off|parked|ChatGPT|Claude/i)
console.log(`Verified ${sources.length} documents, ${diagrams} diagram blocks, ${json} JSON examples and ${links} local links/assets.`)

for (const file of walk('../specs')) {
 const published = file.replace('../specs/', 'public/static/specs/')
 assert.equal(readFileSync(published, 'utf8'), readFileSync(file, 'utf8'), `Stale specification export: ${file}`)
}
assert.ok(readFileSync('public/developers/specifications/index.html','utf8').includes('Specifications'))
const redirects = JSON.parse(readFileSync('redirects.json', 'utf8'))
for (const [old, target] of Object.entries(redirects)) {
 const html = readFileSync('public' + old + '.html', 'utf8')
 assert.ok(html.includes('href="' + target + '"'), 'Missing canonical legacy fallback: ' + old)
 assert.ok(html.includes('location.search + location.hash'), 'Legacy fragment/query loss: ' + old)
}
const index = JSON.parse(readFileSync('public/static/contentIndex.json', 'utf8'))
for (const old of Object.keys(redirects)) assert.ok(!(old.slice(1) in (index.content ?? index)), 'Legacy page leaked into navigation: ' + old)
console.log('Verified exact canonical specification downloads and specification index.')

const home = new JSDOM(readFileSync('public/index.html', 'utf8'))
const functions = JSON.parse(home.window.document.querySelector('.explorer').dataset.dataFns)
// Explorer evaluates the serialized comparator in a fresh browser scope.
const sort = new Function('return (' + functions.sortFn + ')')()
const sections = ['demos', 'developers', 'implementation', 'architecture'].map(name => ({slug: name + '/index', displayName: name}))
assert.deepEqual(sections.sort(sort).map(node => node.displayName), ['architecture', 'implementation', 'developers', 'demos'])
home.window.close()
console.log('Verified serialized sidebar reading order.')

const shareImages = new Set()
const favicon = await sharp('public/static/icon.png').metadata()
assert.equal(favicon.width, 64)
assert.equal(favicon.height, 64)
for (const [file, slug, title] of [
 ['index.html', '/', 'GRAMStack Docs'],
 ['architecture/01-purpose.html', '/architecture/01-purpose', '01 · Purpose and scenario · GRAMStack Docs'],
 ['developers/sdks/index.html', '/developers/sdks/', 'Application SDKs · GRAMStack Docs'],
]) {
 const dom = new JSDOM(readFileSync('public/' + file, 'utf8'))
 const head = dom.window.document.head
 const property = name => head.querySelector(`meta[property="${name}"]`)?.content
 const canonical = 'https://docs.gramstack.try-dough.com' + slug
 assert.equal(property('og:site_name'), 'GRAMStack', file)
 assert.equal(property('og:title'), title, file)
 assert.equal(property('og:url'), canonical, file)
 assert.equal(head.querySelector('link[rel="canonical"]')?.getAttribute('href'), canonical, file)
 const shareImage = property('og:image')
 assert.match(shareImage, /^https:\/\/docs\.gramstack\.try-dough\.com\/static\/share\/[a-f0-9]{16}\.png$/, file)
 assert.equal(property('og:image:url'), shareImage, file)
 assert.equal(head.querySelector('meta[name="twitter:image"]')?.content, shareImage, file)
 const preview = await sharp('public' + new URL(shareImage).pathname).metadata()
 assert.equal(preview.format, 'png', file)
 assert.equal(preview.width, 1200, file)
 assert.equal(preview.height, 630, file)
 shareImages.add(shareImage)
 assert.equal(property('og:image:type'), 'image/png', file)
 assert.match(head.querySelector('link[rel="icon"]')?.getAttribute('href'), /static\/icon\.png\?v=gramstack-1$/, file)
 dom.window.close()
}
assert.equal(shareImages.size, 3, 'Representative pages must have distinct share images')
console.log('Verified page-specific share cards, canonical metadata and favicon.')
