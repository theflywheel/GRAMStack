import http from "node:http"
import { readFile, stat } from "node:fs/promises"
import { resolve, extname, sep } from "node:path"

const root = resolve("public")
const types = { ".html": "text/html; charset=utf-8", ".css": "text/css; charset=utf-8", ".js": "text/javascript; charset=utf-8", ".json": "application/json", ".svg": "image/svg+xml", ".png": "image/png", ".ico": "image/x-icon", ".woff2": "font/woff2", ".xml": "application/xml", ".md": "text/markdown; charset=utf-8", ".txt": "text/plain; charset=utf-8" }
const server = http.createServer(async (req, res) => {
  if (!["GET", "HEAD"].includes(req.method)) { res.writeHead(405, { Allow: "GET, HEAD" }); res.end(); return }
  try {
    const url = new URL(req.url, "http://localhost")
    if (url.pathname === "/healthz") { res.writeHead(200, { "Content-Type": "text/plain" }); res.end(req.method === "HEAD" ? undefined : "ok"); return }
    const pathname = decodeURIComponent(url.pathname)
    if (pathname.includes("\0") || pathname.includes("\\")) { res.writeHead(400); res.end(); return }
    const base = resolve(root, "." + pathname)
    if (base !== root && !base.startsWith(root + sep)) { res.writeHead(403); res.end(); return }
    let file
    for (const candidate of [base, base + ".html", resolve(base, "index.html")]) {
      try { if ((await stat(candidate)).isFile()) { file = candidate; break } } catch {}
    }
    const status = file ? 200 : 404
    const data = await readFile(file ?? resolve(root, "404.html"))
    const headers = { "Content-Type": types[extname(file ?? "404.html")] ?? "application/octet-stream", "Content-Length": data.length, "X-Content-Type-Options": "nosniff", "Referrer-Policy": "strict-origin-when-cross-origin", "X-Robots-Tag": "noindex, nofollow", "Cache-Control": "public, max-age=0, must-revalidate" }
    if (["/static/GRAMStack-Design.md", "/static/gramstack-design.txt"].includes(pathname)) headers["Content-Disposition"] = 'attachment; filename="GRAMStack-Design.md"'
    res.writeHead(status, headers)
    res.end(req.method === "HEAD" ? undefined : data)
  } catch { res.writeHead(400); res.end("Bad request") }
})
server.listen(Number(process.env.PORT ?? 8080), "0.0.0.0", () => console.log(`GRAMStack design: http://localhost:${process.env.PORT ?? 8080}`))
