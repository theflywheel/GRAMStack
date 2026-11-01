import { readFileSync, readdirSync, writeFileSync } from 'node:fs'
const chapters=readdirSync('content').filter(n=>/^\d\d-.*\.md$/.test(n)).sort()
const body=chapters.map(n=>readFileSync(`content/${n}`,'utf8').replace(/^---\n[\s\S]*?\n---\n/, '')).join('\n\n---\n\n')
writeFileSync('content/complete-design.md',`---
title: Complete design · GRAMStack
description: The complete system architecture, including components, protocols, API contracts, state, evidence and retrieval flows.
date: 2026-11-01
---

System architecture · Version 0.13 · 1 November 2026

[Download Markdown](static/gramstack-design.txt).

${body}`)
