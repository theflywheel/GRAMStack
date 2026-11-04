import assert from 'node:assert/strict'
import { spawn } from 'node:child_process'
const port=18081
const child=spawn(process.execPath,['server.mjs'],{env:{...process.env,PORT:String(port)},stdio:['ignore','pipe','pipe']})
try {
 await new Promise((ok,fail)=>{child.stdout.once('data',ok);child.once('error',fail);child.once('exit',c=>fail(new Error(`server exited ${c}`)))})
 const base=`http://127.0.0.1:${port}`
 for(const path of ['/','/02-architecture','/04-consent','/09-dry-runs','/complete-design','/static/contentIndex.json','/healthz']){
   const r=await fetch(base+path);assert.equal(r.status,200,path)
 }
 let r=await fetch(base+'/reference/baseline-hld');assert.equal(r.status,404)
 r=await fetch(base+'/missing-page');assert.equal(r.status,404);assert.match(r.headers.get('content-type'),/text\/html/)
 r=await fetch(base+'/',{method:'POST'});assert.equal(r.status,405)
 r=await fetch(base+'/',{method:'HEAD'});assert.equal(r.status,200);assert.equal(await r.text(),'')
 r=await fetch(base+'/%2e%2e%2fpackage.json');assert.equal(r.status,403)
 r=await fetch(base+'/static/gramstack-design.txt');assert.equal(r.status,200);assert.match(r.headers.get('content-disposition'),/GRAMStack-Design.md/);assert.ok((await r.text()).length>45000)
 r=await fetch(base+'/sakhicredit-demo');assert.equal(r.status,200);const demo=await r.text();assert.match(demo,/<video[^>]+controls/);assert.ok(demo.includes('href="https://docs.gramstack.try-dough.com/media/sakhicredit-hosted.mp4?download=1"'), 'rendered download link')
 r=await fetch(base+'/02-architecture');const frame=/<iframe src="([^"]*architecture-walkthrough(?:\.html)?)"/.exec(await r.text());assert.ok(frame,'rendered architecture iframe')
 r=await fetch(new URL(frame[1],base+'/02-architecture'));assert.equal(r.status,200);assert.match(r.headers.get('content-type'),/text\/html/);assert.ok((await r.text()).includes('GRAMStack architecture and data flow'))
 const movie=base+'/media/sakhicredit-hosted.mp4'
 r=await fetch(movie,{method:'HEAD'});assert.equal(r.status,200);assert.equal(r.headers.get('content-type'),'video/mp4');assert.equal(r.headers.get('accept-ranges'),'bytes')
 const size=Number(r.headers.get('content-length'));assert.ok(size>100000)
 r=await fetch(movie,{headers:{Range:'bytes=0-1'}});assert.equal(r.status,206);assert.equal(r.headers.get('content-range'),`bytes 0-1/${size}`);assert.equal((await r.arrayBuffer()).byteLength,2)
 r=await fetch(movie,{headers:{Range:'bytes=-2'}});assert.equal(r.status,206);assert.equal((await r.arrayBuffer()).byteLength,2)
 r=await fetch(movie,{headers:{Range:`bytes=${size}-`}});assert.equal(r.status,416);assert.equal(r.headers.get('content-range'),`bytes */${size}`)
 r=await fetch(movie+'?download=1');assert.equal(r.status,200);assert.match(r.headers.get('content-disposition'),/^attachment;.*sakhicredit-hosted\.mp4/);assert.equal((await r.arrayBuffer()).byteLength,size)
 console.log('Verified static server: pages, clean URLs, health, search index, 404, HEAD, method rejection, traversal rejection and download.')
} finally { child.kill('SIGTERM') }
