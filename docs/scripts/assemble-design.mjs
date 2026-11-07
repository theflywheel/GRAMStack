import { readFileSync, readdirSync, writeFileSync } from 'node:fs'
const chapters=readdirSync('content/architecture').filter(n=>/^\d\d-.*\.md$/.test(n)).sort()
const body=chapters.map(n=>readFileSync(`content/architecture/${n}`,'utf8').replace(/^---\n[\s\S]*?\n---\n/, '')).join('\n\n---\n\n')
writeFileSync('content/architecture/complete-design.md',`---
title: Complete design · GRAMStack
description: The complete system architecture, including components, protocols, API contracts, state, evidence and retrieval flows.
date: 2026-11-04
---

System architecture · Version 0.15 · 4 November 2026

[Download Markdown](static/gramstack-design.txt).

${body}`)
