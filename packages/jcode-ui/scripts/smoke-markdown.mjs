/**
 * Smoke test for the rich-render markdown pipeline. Run AFTER `pnpm build`:
 *
 *   node scripts/smoke-markdown.mjs
 *
 * Imports the compiled dist (no React, no DOM). In Node there is no `window`,
 * so DOMPurify sanitization is a documented pass-through — the assertions below
 * target marked's output (chrome, streaming completion, math passthrough),
 * which sanitization would only ever strip, never add.
 */

import { renderMarkdown } from '../dist/lib/markdown.js'
import {
  completeStreamingMarkdown,
  renderMarkdownStreaming,
  scanFenceState,
  splitTopLevelBlocks,
  hashString,
  balanceEmphasis,
  stripTrailingLink,
} from '../dist/lib/streamingMarkdown.js'

let passed = 0
let failed = 0

function assert(name, cond, detail) {
  if (cond) {
    passed++
    console.log(`  ok  ${name}`)
  } else {
    failed++
    console.error(`FAIL  ${name}${detail ? `\n      ${detail}` : ''}`)
  }
}

// 1) Unclosed fence → stable, closed HTML with streaming shimmer class.
{
  const md = '```ts\nconst x = 1'
  const html = renderMarkdownStreaming(md)
  assert('unclosed fence → contains </pre>', html.includes('</pre>'), html)
  assert('unclosed fence → codeblock wrapper', html.includes('jcode-codeblock'), html)
  assert('unclosed fence → streaming shimmer class', html.includes('jcode-code-streaming'), html)
  assert('scanFenceState detects the open fence', scanFenceState(md) !== null)
  assert(
    'completeStreamingMarkdown closes the fence',
    scanFenceState(completeStreamingMarkdown(md)) === null,
    completeStreamingMarkdown(md),
  )
}

// 2) Code-block chrome: bar + copy button + filename parsing.
{
  const html = renderMarkdown('```ts title=a.ts\nconst x = 1\n```')
  assert('chrome → bar element', html.includes('jcode-codeblock__bar'), html)
  assert('chrome → copy button', html.includes('jcode-codeblock__copy'), html)
  assert('chrome → Copy label', html.includes('>Copy<'), html)
  assert('chrome → data-code payload', html.includes('data-code="'), html)
  assert('chrome → filename from title=', html.includes('data-filename="a.ts"'), html)
  assert('chrome → lang span', html.includes('>ts<'), html)

  const colon = renderMarkdown('```ts:b.ts\nconst y = 2\n```')
  assert('chrome → filename from lang:file', colon.includes('data-filename="b.ts"'), colon)
}

// 3) Math left literal when katex is NOT registered.
{
  const html = renderMarkdown('inline $x^2$ end and $$a+b$$')
  assert('unregistered inline math preserved', html.includes('$x^2$'), html)
  assert('unregistered block math preserved', html.includes('$$a+b$$'), html)
  assert('no katex markup injected', !html.includes('katex'), html)
}

// 4) Inline-marker completion for prose tails.
{
  assert('balanceEmphasis closes **bold', balanceEmphasis('a **bold').endsWith('**'))
  assert('balanceEmphasis leaves closed **bold**', balanceEmphasis('a **bold**') === 'a **bold**')
  assert('balanceEmphasis ignores snake_case', balanceEmphasis('call foo_bar_baz here') === 'call foo_bar_baz here')
  assert('stripTrailingLink drops dangling [label', stripTrailingLink('see [the doc') === 'see ')
  assert('stripTrailingLink drops dangling [label](url', stripTrailingLink('see [doc](http://x') === 'see ')
  assert('stripTrailingLink keeps completed link', stripTrailingLink('see [doc](http://x)') === 'see [doc](http://x)')
}

// 5) Block splitting + hashing (memoization primitives).
{
  const blocks = splitTopLevelBlocks('# Title\n\npara one\n\n```js\ncode\n```\n\ntail')
  assert('splitTopLevelBlocks → 4 blocks', blocks.length === 4, JSON.stringify(blocks))
  assert('fenced block kept intact', blocks.some((b) => b.includes('```js\ncode\n```')), JSON.stringify(blocks))
  const hashA = hashString('abc')
  const hashB = hashString('abc')
  assert('hashString is deterministic', hashA === hashB)
  assert('hashString distinguishes content', hashString('abc') !== hashString('abd'))
}

console.log(`\n${passed} passed, ${failed} failed`)
process.exit(failed ? 1 : 0)
