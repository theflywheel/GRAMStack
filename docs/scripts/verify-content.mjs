import assert from 'node:assert/strict'
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
assert.doesNotMatch(readFileSync('public/static/contentIndex.json','utf8'), /Historical baseline|LLD|sign.off|parked|ChatGPT|Claude/i)
console.log(`Verified ${sources.length} documents, ${diagrams} diagram blocks, ${json} JSON examples and ${links} local links/assets.`)
