/**
 * useStreamingMarkdown — block-level memoized markdown for streaming messages.
 *
 * A streaming assistant message re-renders on every token. Re-parsing the whole
 * (growing) buffer each frame makes per-frame CPU scale with document length.
 * This hook splits the buffer into top-level blocks, caches the HTML of every
 * completed block (keyed by content hash), and only re-renders the last
 * "active" block — the one still receiving tokens — through the streaming
 * renderer (which also auto-closes its open structures).
 *
 * Returns a single HTML string (blocks joined) for `dangerouslySetInnerHTML`.
 * After injecting it, call `bindCodeBlockCopy(container)` once (see markdown.ts).
 */

import { useRef } from 'react'
import { renderMarkdown } from './markdown.js'
import { hashString, renderMarkdownStreaming, splitTopLevelBlocks } from './streamingMarkdown.js'

export function useStreamingMarkdown(md: string): string {
  const cacheRef = useRef<Map<string, string>>(undefined as unknown as Map<string, string>)
  if (!cacheRef.current) cacheRef.current = new Map()
  const cache = cacheRef.current

  const blocks = splitTopLevelBlocks(md)
  const parts: string[] = []

  for (let i = 0; i < blocks.length; i++) {
    const block = blocks[i]
    const isActive = i === blocks.length - 1

    if (isActive) {
      // The tail block is still streaming: complete + render every frame.
      parts.push(renderMarkdownStreaming(block))
    } else {
      // Completed block: render once, then serve from cache forever (its text
      // is now immutable, so the hash key is stable).
      const key = hashString(block)
      let html = cache.get(key)
      if (html === undefined) {
        html = renderMarkdown(block)
        cache.set(key, html)
      }
      parts.push(html)
    }
  }

  return parts.join('\n')
}
