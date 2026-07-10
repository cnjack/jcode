/**
 * TerminalRenderer — `execute` tool with dual-channel streams/meta support.
 * Head/tail preview, stderr separation, exit/duration badge.
 */

import { memo, useMemo } from 'react'
import type { ToolRendererProps } from 'jcode-ui-core/adapters'

const HEAD_LINES = 5
const TAIL_LINES = 5

export const TerminalRenderer = memo(function TerminalRenderer({
  args,
  output,
  displayOutput,
  error,
  status,
  streams,
  meta,
}: ToolRendererProps) {
  let command = ''
  try {
    const parsed = JSON.parse(args)
    command = parsed.command ?? ''
  } catch {
    // ignore
  }

  const stdout = streams?.stdout ?? ''
  const stderr = streams?.stderr ?? ''
  const hasStreams = !!(stdout || stderr)
  const body = hasStreams
    ? ''
    : displayOutput || output || ''

  const stdoutPreview = useMemo(() => (stdout ? headTail(stdout, HEAD_LINES, TAIL_LINES) : null), [stdout])
  const stderrPreview = useMemo(() => (stderr ? headTail(stderr, HEAD_LINES, TAIL_LINES) : null), [stderr])
  const bodyPreview = useMemo(() => (body ? headTail(body, HEAD_LINES, TAIL_LINES) : null), [body])

  const exitCode = meta?.exit_code
  const durationMs = meta?.duration_ms
  const failed = status === 'error' || (typeof exitCode === 'number' && exitCode !== 0)

  return (
    <div className="jcode-terminal max-h-72 overflow-y-auto px-3 py-2.5 font-mono text-xs leading-relaxed">
      {command && (
        <div className="jcode-terminal__cmd">
          <span className="jcode-terminal__prompt select-none">$ </span>
          <span className="jcode-terminal__command">{command}</span>
        </div>
      )}

      {(typeof exitCode === 'number' || typeof durationMs === 'number') && (
        <div
          className="jcode-terminal__meta mt-1.5 flex flex-wrap gap-2 text-[10px] tabular-nums"
          style={{
            color: failed
              ? 'var(--color-error-fg)'
              : 'color-mix(in srgb, var(--code-fg, var(--color-muted-foreground)) 65%, transparent)',
          }}
        >
          {typeof exitCode === 'number' && (
            <span data-testid="terminal-exit">exit {exitCode}</span>
          )}
          {typeof durationMs === 'number' && durationMs > 0 && (
            <span data-testid="terminal-duration">{formatDuration(durationMs)}</span>
          )}
          {meta?.truncated && <span>truncated</span>}
        </div>
      )}

      {stdoutPreview && (
        <div className="jcode-terminal__out mt-1.5 whitespace-pre-wrap break-all" data-testid="terminal-stdout">
          {stdoutPreview.head}
          {stdoutPreview.omitted > 0 && (
            <div className="jcode-terminal__ellipsis opacity-60">… +{stdoutPreview.omitted} lines</div>
          )}
          {stdoutPreview.tail ? `\n${stdoutPreview.tail}` : null}
        </div>
      )}

      {stderrPreview && (
        <div className="jcode-terminal__err mt-1.5 whitespace-pre-wrap break-all" data-testid="terminal-stderr">
          <div className="jcode-terminal__stream-label mb-0.5 text-[10px] uppercase tracking-wide opacity-80">
            stderr
          </div>
          {stderrPreview.head}
          {stderrPreview.omitted > 0 && (
            <div className="jcode-terminal__ellipsis opacity-60">… +{stderrPreview.omitted} lines</div>
          )}
          {stderrPreview.tail ? `\n${stderrPreview.tail}` : null}
        </div>
      )}

      {!hasStreams && bodyPreview && (
        <div className="jcode-terminal__out mt-1.5 whitespace-pre-wrap break-all">
          {bodyPreview.head}
          {bodyPreview.omitted > 0 && (
            <div className="jcode-terminal__ellipsis opacity-60">… +{bodyPreview.omitted} lines</div>
          )}
          {bodyPreview.tail ? `\n${bodyPreview.tail}` : null}
        </div>
      )}

      {error && <div className="jcode-terminal__err mt-1 whitespace-pre-wrap">{error}</div>}
      {status === 'running' && <div className="jcode-terminal__run mt-1 animate-pulse">Running…</div>}
    </div>
  )
})

function formatDuration(ms: number): string {
  if (ms < 1000) return `${ms}ms`
  return `${(ms / 1000).toFixed(1)}s`
}

/** Keep head + tail lines with an omitted count (Codex-style mid ellipsis). */
export function headTail(
  text: string,
  head: number,
  tail: number,
): { head: string; tail: string; omitted: number } {
  const lines = text.replace(/\n$/, '').split('\n')
  if (lines.length <= head + tail) {
    return { head: lines.join('\n'), tail: '', omitted: 0 }
  }
  const top = lines.slice(0, head)
  const bottom = lines.slice(-tail)
  const omitted = lines.length - head - tail
  return { head: top.join('\n'), tail: bottom.join('\n'), omitted }
}

/** Count code points (not UTF-16 units) so CJK truncation is fair. */
export function truncate(text: string, max: number): string {
  const chars = [...text]
  if (chars.length <= max) return text
  return chars.slice(0, max).join('') + `… (${chars.length} chars)`
}
