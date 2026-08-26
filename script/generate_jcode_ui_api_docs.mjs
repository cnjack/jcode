#!/usr/bin/env node
/**
 * Generate site/docs/chat-ui/api/generated.md from jcode-ui source.
 *
 * Scans packages/jcode-ui/src and packages/jcode-ui-core/src for .ts/.tsx.
 * Extracts export interface|type|class|function declarations (with leading
 * JSDoc) and writes a single markdown page the docs site ships.
 *
 * Usage:
 *   node script/generate_jcode_ui_api_docs.mjs
 */

import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const __dirname = path.dirname(fileURLToPath(import.meta.url))
const root = path.resolve(__dirname, '..')
const outFile = path.join(root, 'site/docs/chat-ui/api/generated.md')

const SCAN = [
  'packages/jcode-ui/src',
  'packages/jcode-ui-core/src',
]

/** @param {string} dir */
function walk(dir) {
  /** @type {string[]} */
  const out = []
  if (!fs.existsSync(dir)) return out
  for (const ent of fs.readdirSync(dir, { withFileTypes: true })) {
    const p = path.join(dir, ent.name)
    if (ent.isDirectory()) {
      if (ent.name === 'node_modules' || ent.name === 'dist') continue
      out.push(...walk(p))
    } else if (/\.(ts|tsx)$/.test(ent.name) && !ent.name.endsWith('.test.ts') && !ent.name.endsWith('.test.tsx')) {
      out.push(p)
    }
  }
  return out
}

/**
 * @typedef {{ kind: string, name: string, signature: string, doc: string, file: string, pkg: string }} Symbol
 */

/**
 * @param {string} file
 * @param {string} src
 * @returns {Symbol[]}
 */
export function extractSymbols(file, src) {
  const rel = path.relative(root, file)
  const pkg = rel.startsWith('packages/jcode-ui-core') ? 'jcode-ui-core' : 'jcode-ui'
  /** @type {Symbol[]} */
  const symbols = []

  // Match export interface / type / class / function with optional JSDoc above.
  // The doc group must FORBID '*/' inside — a lazy [\s\S]*? backtracks past
  // the first '*/' when the following text isn't `export`, welding an earlier
  // comment + intervening code onto the next exported symbol's doc.
  const re =
    /(?:\/\*\*((?:(?!\*\/)[\s\S])*)\*\/\s*)?export\s+(?:declare\s+)?(interface|type|class|function|const)\s+([A-Za-z0-9_]+)/g

  let m
  while ((m = re.exec(src))) {
    const docRaw = m[1] ?? ''
    const kind = m[2]
    const name = m[3]
    // Skip internal re-export noise / React memo wrappers named oddly
    if (name === 'default') continue

    const start = m.index + m[0].length
    let signature = ''

    if (kind === 'interface' || kind === 'class') {
      // Capture until matching brace depth returns to 0 from first {
      const braceStart = src.indexOf('{', start - 1)
      if (braceStart === -1) continue
      let depth = 0
      let i = braceStart
      for (; i < src.length; i++) {
        const ch = src[i]
        if (ch === '{') depth++
        else if (ch === '}') {
          depth--
          if (depth === 0) {
            i++
            break
          }
        }
      }
      signature = src.slice(m.index + (m[1] ? m[0].indexOf('export') : 0), i).replace(/^[\s\S]*?export\s+/, 'export ')
      // Prefer from "export interface" only
      const expIdx = src.lastIndexOf('export', braceStart)
      signature = src.slice(expIdx, i).trim()
    } else if (kind === 'type') {
      // type Foo = ... until an unbracketed semicolon or declaration boundary
      let i = start
      let depth = 0
      let angle = 0
      for (; i < src.length; i++) {
        const ch = src[i]
        if (ch === '{' || ch === '(' || ch === '[') depth++
        else if (ch === '}' || ch === ')' || ch === ']') depth--
        else if (ch === '<') angle++
        else if (ch === '>') angle--
        else if (ch === ';' && depth <= 0 && angle <= 0) {
          i++
          break
        }
        else if (ch === '\n' && depth <= 0 && angle <= 0) {
          const nextLine = src.slice(i + 1)
          // Semicolon-free aliases end at the next top-level declaration (or
          // its JSDoc). Only skip horizontal whitespace here: consuming
          // newlines would make a multiline union look like it had ended.
          if (/^[\t ]*(?:\r?\n|export\b|\/\*\*)/.test(nextLine)) break
        }
      }
      const expIdx = src.lastIndexOf('export', start)
      signature = src.slice(expIdx, i).trim()
      if (!signature.endsWith(';')) signature += ';'
    } else if (kind === 'function') {
      // Signature = "export function name<…>(args): Ret" up to the body brace.
      // Count ONLY parentheses: '<'/'>' also appear in arrows (=>) and
      // comparisons, and counting them corrupted the depth so the scan ran
      // past the body and swallowed the following declarations.
      let i = start
      let paren = 0
      let sawParens = false
      for (; i < src.length; i++) {
        const ch = src[i]
        if (ch === '(') paren++
        else if (ch === ')') {
          paren--
          if (paren === 0) sawParens = true
        } else if (paren === 0 && sawParens && ch === '{') {
          // body starts — signature ends here
          break
        } else if (paren === 0 && sawParens && ch === ';') {
          i++
          break
        }
      }
      const expIdx = src.lastIndexOf('export', start)
      signature = src.slice(expIdx, i).trim()
      if (signature.endsWith('{')) signature = signature.slice(0, -1).trim() + ' { … }'
      else if (!signature.endsWith(';') && !signature.endsWith('}')) signature += ' { … }'
    } else if (kind === 'const') {
      // Only keep function-like consts: export const X = memo( or (
      const snippet = src.slice(m.index, m.index + 200)
      if (!/=\s*(?:memo\s*)?\(/.test(snippet) && !/=\s*function/.test(snippet)) continue
      const expIdx = m.index
      // Take first line-ish of const export
      const lineEnd = src.indexOf('\n', start)
      signature = src.slice(expIdx, lineEnd === -1 ? start + 80 : lineEnd).trim()
      if (!signature.includes('(')) continue
      signature = signature.replace(/=\s*memo\(.*/, '= …')
      signature = signature.replace(/=\s*\(.*/, '= …')
    }

    // Normalize signature length
    if (signature.length > 4000) signature = signature.slice(0, 4000) + '\n  /* … truncated */\n}'

    const doc = cleanDoc(docRaw)
    symbols.push({ kind, name, signature, doc, file: rel, pkg })
  }

  return symbols
}

/** @param {string} raw */
function cleanDoc(raw) {
  return raw
    .split('\n')
    .map((l) => l.replace(/^\s*\*\s?/, '').replace(/^\s*\*/, ''))
    .join('\n')
    .trim()
}

/** @param {string} s */
function esc(s) {
  return s.replace(/\|/g, '\\|')
}

/** @param {string} value */
function slug(value) {
  return value.toLowerCase().replace(/[^a-z0-9]+/g, '-')
}

/**
 * @param {Symbol[]} symbols
 * @param {string} generatedAt
 */
export function renderMarkdown(symbols, generatedAt = new Date().toISOString().slice(0, 10)) {
  const globalNameCounts = new Map()
  for (const s of symbols) globalNameCounts.set(s.name, (globalNameCounts.get(s.name) ?? 0) + 1)

  const byPkg = {
    'jcode-ui': symbols.filter((s) => s.pkg === 'jcode-ui'),
    'jcode-ui-core': symbols.filter((s) => s.pkg === 'jcode-ui-core'),
  }

  let md = `---
title: Generated API
parent: API Reference
nav_order: 6
---

# Generated API

> Auto-generated from TypeScript sources on **${generatedAt}**.
> Do not edit by hand — run \`node script/generate_jcode_ui_api_docs.mjs\`.
>
> Human-written guides: [Types](/chat-ui/docs/api/types) · [Runtime](/chat-ui/docs/api/runtime) · [Hooks](/chat-ui/docs/api/hooks) · [Primitives](/chat-ui/docs/api/primitives) · [Components](/chat-ui/docs/api/components).

**${symbols.length}** public symbols extracted.

`

  for (const [pkg, list] of Object.entries(byPkg)) {
    const nameCounts = new Map()
    for (const s of list) nameCounts.set(s.name, (nameCounts.get(s.name) ?? 0) + 1)

    const presentation = list.map((s) => {
      const baseId = slug(`${pkg}-${s.name}`)
      const collided = (nameCounts.get(s.name) ?? 0) > 1
      const headingQualifier = collided
        ? s.kind
        : (globalNameCounts.get(s.name) ?? 0) > 1
          ? pkg
          : ''
      return {
        symbol: s,
        baseId,
        collided,
        headingId: collided ? slug(`${pkg}-${s.name}-${s.kind}`) : baseId,
        heading: headingQualifier ? `\`${s.name}\` (${headingQualifier})` : `\`${s.name}\``,
      }
    })

    md += `## \`${pkg}\`\n\n`
    md += `| Symbol | Kind | Source |\n|--------|------|--------|\n`
    for (const item of presentation) {
      const { symbol: s } = item
      md += `| [${item.heading}](#${item.headingId}) | ${s.kind} | \`${esc(s.file)}\` |\n`
    }
    md += `\n`

    const emittedLegacyAnchors = new Set()
    for (const item of presentation) {
      const { symbol: s } = item
      // Existing links used pkg+name. Keep that legacy alias once for a
      // collision while each concrete symbol gets a unique pkg+name+kind ID.
      if (item.collided && !emittedLegacyAnchors.has(item.baseId)) {
        md += `<a id="${item.baseId}"></a>\n\n`
        emittedLegacyAnchors.add(item.baseId)
      }
      md += `<a id="${item.headingId}"></a>\n\n`
      md += `### ${item.heading}\n\n`
      md += `\`${s.kind}\` · \`${s.file}\`\n\n`
      if (s.doc) {
        md += `${s.doc}\n\n`
      }
      md += '```ts\n' + s.signature + '\n```\n\n'
    }
  }

  return md
}

function main() {
  /** @type {Symbol[]} */
  const all = []
  for (const dir of SCAN) {
    for (const file of walk(path.join(root, dir))) {
      const src = fs.readFileSync(file, 'utf8')
      all.push(...extractSymbols(file, src))
    }
  }

  // Dedupe by pkg+kind+name (prefer longer signature / with doc)
  /** @type {Map<string, Symbol>} */
  const map = new Map()
  for (const s of all) {
    const key = `${s.pkg}::${s.kind}::${s.name}`
    const prev = map.get(key)
    if (!prev || (s.doc && !prev.doc) || s.signature.length > prev.signature.length) {
      map.set(key, s)
    }
  }
  const symbols = [...map.values()].sort((a, b) => {
    if (a.pkg !== b.pkg) return a.pkg.localeCompare(b.pkg)
    if (a.kind !== b.kind) return a.kind.localeCompare(b.kind)
    return a.name.localeCompare(b.name)
  })

  const md = renderMarkdown(symbols)

  fs.mkdirSync(path.dirname(outFile), { recursive: true })
  fs.writeFileSync(outFile, md)
  console.log(`Wrote ${symbols.length} symbols → ${path.relative(root, outFile)}`)
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) main()
