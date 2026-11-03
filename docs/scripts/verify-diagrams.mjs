import assert from 'node:assert/strict'
import { readFileSync, readdirSync } from 'node:fs'
import { JSDOM } from 'jsdom'

// Parsing needs sanitization DOM APIs, but no browser, layout or network access.
const { window } = new JSDOM('')
globalThis.window = window
globalThis.document = window.document
const { default: mermaid } = await import('mermaid')
mermaid.initialize({ startOnLoad: false, securityLevel: 'strict' })
const walk = path => readdirSync(path, { withFileTypes: true }).flatMap(entry =>
  entry.isDirectory() ? walk(`${path}/${entry.name}`) : [`${path}/${entry.name}`])
const seen = new Set()
let blocks = 0
try {
  // Regression: a semicolon inside a sequence note starts an invalid statement.
  await assert.rejects(mermaid.parse('sequenceDiagram\nparticipant A\nNote over A: first; invalid statement'))
  for (const file of walk('content').filter(file => file.endsWith('.md')).sort()) {
    let index = 0
    for (const match of readFileSync(file, 'utf8').matchAll(/^```mermaid\s*\n([\s\S]*?)^```/gm)) {
      index++; blocks++
      const source = match[1].trim()
      if (seen.has(source)) continue // Complete-design exports repeat chapter diagrams.
      try { await mermaid.parse(source) }
      catch (error) { throw new Error(`${file}, Mermaid diagram ${index}: ${error.message}`) }
      seen.add(source)
    }
  }
  assert.ok(seen.size > 0, 'No Mermaid definitions checked')
  console.log(`Parsed ${seen.size} unique Mermaid diagrams across ${blocks} blocks with strict sanitization.`)
} catch (error) {
  console.error(error.message)
  process.exitCode = 1
} finally {
  window.close()
}
