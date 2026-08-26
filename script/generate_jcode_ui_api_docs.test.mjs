import assert from 'node:assert/strict'
import path from 'node:path'
import { describe, it } from 'node:test'
import { fileURLToPath } from 'node:url'

import { extractSymbols, renderMarkdown } from './generate_jcode_ui_api_docs.mjs'

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const fixtureFile = path.join(root, 'packages/jcode-ui-core/src/types/generator-fixture.ts')

describe('extractSymbols', () => {
  it('stops a semicolon-free type alias at the next exported declaration', () => {
    const symbols = extractSymbols(
      fixtureFile,
      `export type ToolPhase = 'queued' | 'terminal'
export type ToolOutcome = 'succeeded' | 'failed'
`,
    )

    assert.deepEqual(
      symbols.map(({ name, signature }) => ({ name, signature })),
      [
        { name: 'ToolPhase', signature: "export type ToolPhase = 'queued' | 'terminal';" },
        { name: 'ToolOutcome', signature: "export type ToolOutcome = 'succeeded' | 'failed';" },
      ],
    )
  })

  it('keeps multiline unions together and leaves the next JSDoc on its symbol', () => {
    const symbols = extractSymbols(
      fixtureFile,
      `export type Multi =
  | 'first'
  | 'second'
/** Outcome docs. */
export type Outcome = 'done'
`,
    )

    assert.equal(symbols[0].signature, "export type Multi =\n  | 'first'\n  | 'second';")
    assert.equal(symbols[0].doc, '')
    assert.equal(symbols[1].signature, "export type Outcome = 'done';")
    assert.equal(symbols[1].doc, 'Outcome docs.')
  })
})

describe('renderMarkdown', () => {
  it('gives same-name symbols unique headings and anchors while preserving one legacy alias', () => {
    const base = { doc: '', pkg: 'jcode-ui' }
    const markdown = renderMarkdown(
      [
        {
          ...base,
          kind: 'const',
          name: 'ChatInput',
          signature: 'export const ChatInput = …',
          file: 'packages/jcode-ui/src/components/ChatInput.tsx',
        },
        {
          ...base,
          kind: 'function',
          name: 'ChatInput',
          signature: 'export function ChatInput() { … }',
          file: 'packages/jcode-ui/src/product/ChatInput.tsx',
        },
      ],
      '2026-08-26',
    )

    assert.match(markdown, /\[`ChatInput` \(const\)\]\(#jcode-ui-chatinput-const\)/)
    assert.match(markdown, /\[`ChatInput` \(function\)\]\(#jcode-ui-chatinput-function\)/)
    assert.match(markdown, /^### `ChatInput` \(const\)$/m)
    assert.match(markdown, /^### `ChatInput` \(function\)$/m)

    const anchors = [...markdown.matchAll(/<a id="([^"]+)"><\/a>/g)].map((match) => match[1])
    assert.equal(anchors.filter((id) => id === 'jcode-ui-chatinput').length, 1)
    assert.equal(new Set(anchors).size, anchors.length)
  })

  it('keeps the existing anchor and heading for a non-conflicting symbol', () => {
    const markdown = renderMarkdown(
      [
        {
          kind: 'const',
          name: 'Thread',
          signature: 'export const Thread = …',
          doc: '',
          file: 'packages/jcode-ui/src/components/Thread.tsx',
          pkg: 'jcode-ui',
        },
      ],
      '2026-08-26',
    )

    assert.match(markdown, /\[`Thread`\]\(#jcode-ui-thread\)/)
    assert.match(markdown, /<a id="jcode-ui-thread"><\/a>/)
    assert.match(markdown, /^### `Thread`$/m)
  })

  it('qualifies cross-package duplicate headings without changing their anchors', () => {
    const markdown = renderMarkdown(
      [
        {
          kind: 'const',
          name: 'Thread',
          signature: 'export const Thread = …',
          doc: '',
          file: 'packages/jcode-ui/src/components/Thread.tsx',
          pkg: 'jcode-ui',
        },
        {
          kind: 'interface',
          name: 'Thread',
          signature: 'export interface Thread {}',
          doc: '',
          file: 'packages/jcode-ui-core/src/types/index.ts',
          pkg: 'jcode-ui-core',
        },
      ],
      '2026-08-26',
    )

    assert.match(markdown, /^### `Thread` \(jcode-ui\)$/m)
    assert.match(markdown, /^### `Thread` \(jcode-ui-core\)$/m)
    assert.match(markdown, /<a id="jcode-ui-thread"><\/a>/)
    assert.match(markdown, /<a id="jcode-ui-core-thread"><\/a>/)
    assert.doesNotMatch(markdown, /jcode-ui-thread-(?:const|jcode-ui)/)
  })
})
