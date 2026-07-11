/**
 * Streaming-stable markdown.
 *
 * While an assistant message streams token-by-token, the tail of the buffer is
 * almost always mid-structure — an open code fence, a half-typed `**bold`, a
 * dangling `[link`. Rendering that raw produces flicker (a stray ``` turns the
 * rest of the doc into a code block for one frame). `completeStreamingMarkdown`
 * closes those open structures so every intermediate frame is valid markdown.
 *
 * All helpers are pure and individually exported for unit testing.
 */

import { renderMarkdown } from './markdown.js'

/** Matches a line that opens or closes a fenced code block (indent ≤ 3). */
const FENCE_LINE = /^(\s{0,3})(`{3,}|~{3,})(.*)$/

interface Fence {
  char: string
  len: number
}

/**
 * Scan for an unterminated code fence. Returns the open fence descriptor (so the
 * caller knows what to close with), or `null` when all fences are balanced.
 */
export function scanFenceState(md: string): Fence | null {
  let open: Fence | null = null
  for (const line of md.split('\n')) {
    const m = FENCE_LINE.exec(line)
    if (!m) continue
    const char = m[2][0]
    const len = m[2].length
    const info = m[3]
    if (open == null) {
      open = { char, len }
    } else if (char === open.char && len >= open.len && info.trim() === '') {
      open = null
    }
    // otherwise: a fence-looking line inside the block that is not a valid
    // closer → treat as content.
  }
  return open
}

/** Drop the contents of *closed* fenced blocks (used for delimiter counting). */
function stripFenced(text: string): string {
  const out: string[] = []
  let open: Fence | null = null
  for (const line of text.split('\n')) {
    const m = FENCE_LINE.exec(line)
    if (m) {
      const char = m[2][0]
      const len = m[2].length
      const info = m[3]
      if (open == null) {
        open = { char, len }
        continue
      }
      if (char === open.char && len >= open.len && info.trim() === '') {
        open = null
        continue
      }
    }
    if (open == null) out.push(line)
  }
  return out.join('\n')
}

/** Blank out inline code spans so their delimiters don't skew emphasis counts. */
function stripInlineCode(text: string): string {
  return text.replace(/`+[^`]*`+/g, ' ')
}

/**
 * Close a dangling inline code span. Counts backticks in prose (outside fenced
 * blocks); an odd count means one span is open, so append a closing backtick.
 */
export function balanceInlineCode(text: string): string {
  const ticks = (stripFenced(text).match(/`/g) || []).length
  return ticks % 2 === 1 ? text + '`' : text
}

/**
 * Close dangling emphasis runs (`**`, `*`, `_`). Counts are taken over prose
 * only (code stripped). Underscores are counted only when they flank a word
 * boundary, so `snake_case` and URLs don't trigger a false close.
 */
export function balanceEmphasis(text: string): string {
  const prose = stripInlineCode(stripFenced(text))
  let out = text

  const bold = (prose.match(/\*\*/g) || []).length
  if (bold % 2 === 1) out += '**'

  const star = (prose.replace(/\*\*/g, '').match(/\*/g) || []).length
  if (star % 2 === 1) out += '*'

  const under = (
    prose.match(/(?:^|[\s.,!?;:(){}[\]"'*~])_(?=\S)|(?<=\S)_(?=$|[\s.,!?;:(){}[\]"'*~])/g) || []
  ).length
  if (under % 2 === 1) out += '_'

  return out
}

/**
 * Remove a half-typed link/image at the very end of the buffer:
 *   `[label`         → dangling label
 *   `[label](url`    → dangling destination
 * Leaves completed links untouched.
 */
export function stripTrailingLink(text: string): string {
  let m = /!?\[[^[\]]*$/.exec(text)
  if (m) return text.slice(0, m.index)
  m = /!?\[[^[\]]*\]\([^()]*$/.exec(text)
  if (m) return text.slice(0, m.index)
  return text
}

/** Add the trailing pipe to a table row that is still being typed. */
export function completeTableRow(text: string): string {
  const nl = text.lastIndexOf('\n')
  const last = text.slice(nl + 1).trim()
  if (last.length > 1 && last.startsWith('|') && !last.endsWith('|')) {
    return text + ' |'
  }
  return text
}

export interface CompletionResult {
  text: string
  /** True when an open code fence was auto-closed (last block is streaming). */
  fenceStreaming: boolean
}

/**
 * Complete unclosed markdown structures. Reports whether a code fence was closed
 * so the renderer can flag the active code block (shimmer). See the module doc.
 */
export function completeStreamingMarkdownInfo(md: string): CompletionResult {
  const fence = scanFenceState(md)
  if (fence) {
    const closer = fence.char.repeat(fence.len)
    const sep = md.length === 0 || md.endsWith('\n') ? '' : '\n'
    // Inside a fence the content is literal — do NOT balance inline markers.
    return { text: md + sep + closer, fenceStreaming: true }
  }
  let text = md
  text = stripTrailingLink(text)
  text = balanceInlineCode(text)
  text = balanceEmphasis(text)
  text = completeTableRow(text)
  return { text, fenceStreaming: false }
}

/** Pure completion: raw streaming buffer → renderable markdown string. */
export function completeStreamingMarkdown(md: string): string {
  return completeStreamingMarkdownInfo(md).text
}

/** Tag the last `.jcode-codeblock` as streaming so CSS can shimmer its tail. */
function markStreamingCodeblock(html: string): string {
  const MARK = 'class="jcode-codeblock"'
  const i = html.lastIndexOf(MARK)
  if (i < 0) return html
  return html.slice(0, i) + 'class="jcode-codeblock jcode-code-streaming"' + html.slice(i + MARK.length)
}

/**
 * Render a streaming buffer to sanitized HTML. Equivalent to
 * `renderMarkdown(completeStreamingMarkdown(md))`, plus a shimmer class on the
 * code block that is still streaming.
 */
export function renderMarkdownStreaming(md: string): string {
  const { text, fenceStreaming } = completeStreamingMarkdownInfo(md)
  const html = renderMarkdown(text)
  return fenceStreaming ? markStreamingCodeblock(html) : html
}

// ─── Block-level memoization ────────────────────────────────────────────────

/**
 * Split markdown into top-level blocks on blank lines, keeping fenced code
 * blocks intact. Used to memoize completed blocks during streaming so per-frame
 * work is O(active block) instead of O(whole document).
 */
export function splitTopLevelBlocks(md: string): string[] {
  const blocks: string[] = []
  let current: string[] = []
  let open: Fence | null = null

  const flush = () => {
    if (current.length) {
      blocks.push(current.join('\n'))
      current = []
    }
  }

  for (const line of md.split('\n')) {
    const m = FENCE_LINE.exec(line)
    if (m) {
      const char = m[2][0]
      const len = m[2].length
      const info = m[3]
      if (open == null) open = { char, len }
      else if (char === open.char && len >= open.len && info.trim() === '') open = null
      current.push(line)
      continue
    }
    if (open == null && line.trim() === '') {
      flush()
      continue
    }
    current.push(line)
  }
  flush()
  return blocks.length ? blocks : ['']
}

/** Fast, stable string hash (djb2 + length) for block cache keys. */
export function hashString(s: string): string {
  let h = 5381
  for (let i = 0; i < s.length; i++) {
    h = ((h << 5) + h + s.charCodeAt(i)) | 0
  }
  return (h >>> 0).toString(36) + ':' + s.length.toString(36)
}
