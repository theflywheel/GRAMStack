import { createHash } from 'node:crypto'
import { mkdir, readdir, readFile, writeFile } from 'node:fs/promises'
import { join } from 'node:path'
import { JSDOM } from 'jsdom'
import sharp from 'sharp'

const cardDirectory = 'static/share'
const fallbackImage = '/static/gramstack-share-v1.png'

function excerpt(text, limit) {
  const normalized = text.replace(/\s+/gu, ' ').trim()
  const characters = Array.from(normalized)
  if (characters.length <= limit) return normalized
  const shortened = characters.slice(0, limit).join('')
  const lastSpace = shortened.lastIndexOf(' ')
  return (lastSpace > limit * 0.8 ? shortened.slice(0, lastSpace) : shortened) + '…'
}

async function textLayer(text, { color, font, height }) {
  const escaped = text.replaceAll('&', '&amp;').replaceAll('<', '&lt;').replaceAll('>', '&gt;')
  const width = 1004
  const rendered = await sharp({
    text: {
      text: `<span foreground="${color}">${escaped}</span>`,
      font,
      width,
      wrap: 'word-char',
      rgba: true,
    },
  }).png().toBuffer()
  return sharp(rendered).resize({ width, height, fit: 'inside', withoutEnlargement: true }).png().toBuffer()
}

export async function renderShareCards(root = 'public') {
  const frame = await sharp(await readFile(new URL('./share-card.svg', import.meta.url))).png().toBuffer()
  await mkdir(join(root, cardDirectory), { recursive: true })
  let generated = 0
  for (const file of await readdir(root, { recursive: true })) {
    if (!file.endsWith('.html') || file === '404.html') continue
    const page = join(root, file)
    const html = await readFile(page, 'utf8')
    const start = html.indexOf('<head>')
    const end = html.indexOf('</head>') + '</head>'.length
    if (start < 0 || end <= start) continue
    const dom = new JSDOM(html.slice(start, end))
    try {
      const head = dom.window.document.head
      const meta = property => head.querySelector(`meta[property="${property}"]`)
      const image = meta('og:image')
      if (!image || !image.content.endsWith(fallbackImage)) continue

      const title = meta('og:title').content.replace(/ · GRAMStack Docs$/, '')
      const description = meta('og:description').content
      const layers = [{
        input: await textLayer(excerpt(title, 126), { color: '#eef4ed', font: 'Sans Bold 82', height: 164 }),
        top: 205, left: 98,
      }]
      if (description.trim()) {
        layers.push({
          input: await textLayer(excerpt(description, 220), { color: '#c4d5ca', font: 'Sans 36', height: 102 }),
          top: 376, left: 98,
        })
      }
      const png = await sharp(frame).composite(layers).png().toBuffer()
      const version = createHash('sha256').update(png).digest('hex').slice(0, 16)
      const card = `${cardDirectory}/${version}.png`
      await writeFile(join(root, card), png)
      const url = new URL('/' + card, image.content).href
      image.content = url
      meta('og:image:url').content = url
      head.querySelector('meta[name="twitter:image"]').content = url
      meta('og:image:alt').content = `${title} · GRAMStack Documentation`
      await writeFile(page, html.slice(0, start) + head.outerHTML + html.slice(end))
      generated++
    } finally {
      dom.window.close()
    }
  }
  if (generated === 0) throw new Error('No docs share cards were generated')
  console.log(`Generated ${generated} page-specific share cards.`)
}
