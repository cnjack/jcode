/**
 * StackTraceRenderer — recognizes Go panic stacks and JS Error stacks and
 * renders a structured frame list: function name emphasized, `file:line` in a
 * muted mono, and a leading error message.
 *
 * Frames with a resolvable `file:line` are clickable — clicking dispatches a
 * bubbling `CustomEvent('jcode-ui:open-file', { detail: { path, line } })` on
 * the element. Hosts embedded in an editor (VS Code webview, desktop shell) can
 * listen on a document/root ancestor and jump to the location, e.g.:
 *
 *   root.addEventListener('jcode-ui:open-file', (e) => {
 *     const { path, line } = (e as CustomEvent).detail
 *     openInEditor(path, line)
 *   })
 *
 * node_modules / language-runtime frames are collapsed by default behind a
 * "N hidden frames" toggle. `parseStackTrace` is exported for reuse.
 */

import { memo, useMemo, useState } from 'react'
import type { ToolRendererProps } from 'jcode-ui-core/adapters'

export interface StackFrame {
  func: string
  file?: string
  line?: number
  column?: number
  raw: string
  /** node_modules or language-runtime frame — collapsed by default. */
  isRuntime: boolean
}

export interface StackTrace {
  kind: 'go' | 'js'
  message: string
  frames: StackFrame[]
}

export const StackTraceRenderer = memo(function StackTraceRenderer({
  output,
  displayOutput,
  error,
  status,
}: ToolRendererProps) {
  const raw = error || displayOutput || output || ''
  const trace = useMemo(() => parseStackTrace(raw), [raw])

  if (!trace) {
    if (status === 'running') {
      return <div className="jcode-stacktrace__msg animate-pulse">Running…</div>
    }
    return <pre className="jcode-stacktrace__raw">{raw}</pre>
  }

  const runtimeCount = trace.frames.filter((f) => f.isRuntime).length
  return <StackView trace={trace} runtimeCount={runtimeCount} />
})

function StackView({ trace, runtimeCount }: { trace: StackTrace; runtimeCount: number }) {
  const [showRuntime, setShowRuntime] = useState(false)

  const openFile = (frame: StackFrame, target: EventTarget & HTMLElement) => {
    if (!frame.file || frame.line == null) return
    target.dispatchEvent(
      new CustomEvent('jcode-ui:open-file', {
        detail: { path: frame.file, line: frame.line },
        bubbles: true,
      }),
    )
  }

  return (
    <div data-jcode-ui="" className={`jcode-stacktrace jcode-stacktrace--${trace.kind}`}>
      {trace.message && <div className="jcode-stacktrace__message">{trace.message}</div>}
      <ul className="jcode-stacktrace__frames">
        {trace.frames.map((frame, i) => {
          if (frame.isRuntime && !showRuntime) return null
          const clickable = !!frame.file && frame.line != null
          return (
            <li key={i} className={`jcode-stacktrace__frame${frame.isRuntime ? ' jcode-stacktrace__frame--runtime' : ''}`}>
              <button
                type="button"
                className="jcode-stacktrace__frame-row"
                disabled={!clickable}
                onClick={(e) => openFile(frame, e.currentTarget)}
                title={clickable ? `Open ${frame.file}:${frame.line}` : undefined}
              >
                <span className="jcode-stacktrace__func">{frame.func || '(anonymous)'}</span>
                {frame.file && (
                  <span className="jcode-stacktrace__loc">
                    {shortenPath(frame.file)}
                    {frame.line != null && `:${frame.line}`}
                    {frame.column != null && `:${frame.column}`}
                  </span>
                )}
              </button>
            </li>
          )
        })}
      </ul>
      {runtimeCount > 0 && (
        <button
          type="button"
          className="jcode-stacktrace__toggle"
          onClick={() => setShowRuntime((v) => !v)}
        >
          {showRuntime ? 'Hide' : 'Show'} {runtimeCount} runtime frame{runtimeCount === 1 ? '' : 's'}
        </button>
      )}
    </div>
  )
}

function shortenPath(path: string): string {
  const parts = path.split('/')
  if (parts.length <= 3) return path
  return '…/' + parts.slice(-3).join('/')
}

const JS_AT = /^\s*at\s+(.*)$/
const JS_LOC = /^(.*?)\s*\(?([^()\s]+):(\d+):(\d+)\)?$/
const GO_FILE = /^\s+(\S+):(\d+)(?:\s+\+0x[0-9a-f]+)?\s*$/

function isRuntimeFile(file: string): boolean {
  return (
    /node_modules/.test(file) ||
    /^node:/.test(file) ||
    /(^|\/)internal\//.test(file) ||
    /\/go\/src\/runtime\//.test(file) ||
    /\/src\/runtime\//.test(file) ||
    /GOROOT/.test(file)
  )
}

export function parseStackTrace(text: string): StackTrace | null {
  if (!text.trim()) return null
  const lines = text.split('\n')

  const isJs = /^\s*at\s+/m.test(text)
  const isGo = /^goroutine\s+\d+\s+\[|^panic:|^\t.*\.go:\d+/m.test(text)

  if (isJs && !isGo) return parseJs(lines)
  if (isGo) return parseGo(lines)
  return null
}

function parseJs(lines: string[]): StackTrace | null {
  const frames: StackFrame[] = []
  let message = ''
  for (const line of lines) {
    const at = JS_AT.exec(line)
    if (!at) {
      if (!message && line.trim() && !/^\s*$/.test(line)) message = line.trim()
      continue
    }
    const body = (at[1] ?? '').trim()
    const loc = JS_LOC.exec(body)
    if (loc) {
      const func = (loc[1] ?? '').trim()
      const file = loc[2] ?? ''
      frames.push({
        func,
        file,
        line: loc[3] ? parseInt(loc[3], 10) : undefined,
        column: loc[4] ? parseInt(loc[4], 10) : undefined,
        raw: line,
        isRuntime: isRuntimeFile(file),
      })
    } else {
      frames.push({ func: body, raw: line, isRuntime: false })
    }
  }
  if (frames.length === 0) return null
  return { kind: 'js', message, frames }
}

function parseGo(lines: string[]): StackTrace | null {
  const frames: StackFrame[] = []
  let message = ''
  let pendingFunc: string | null = null

  for (const line of lines) {
    if (!message && /^(panic:|fatal error:)/.test(line)) {
      message = line.trim()
      continue
    }
    if (/^goroutine\s+\d+/.test(line)) {
      pendingFunc = null
      continue
    }
    const fileMatch = GO_FILE.exec(line)
    if (fileMatch) {
      const file = fileMatch[1] ?? ''
      const func = (pendingFunc ?? '').replace(/\(.*\)$/, '').trim()
      frames.push({
        func: pendingFunc ? cleanGoFunc(pendingFunc) : '',
        file,
        line: fileMatch[2] ? parseInt(fileMatch[2], 10) : undefined,
        raw: line,
        isRuntime: isRuntimeFile(file) || /^runtime\./.test(func),
      })
      pendingFunc = null
      continue
    }
    // A function line: `pkg.Func(0x1, 0x2)` or `created by pkg.Func`.
    if (line.trim() && !/^\s/.test(line)) {
      pendingFunc = line.trim()
    } else if (line.trim() && pendingFunc == null) {
      pendingFunc = line.trim()
    }
  }
  if (frames.length === 0) return null
  return { kind: 'go', message, frames }
}

function cleanGoFunc(fn: string): string {
  return fn.replace(/^created by\s+/, '').replace(/\(.*\)$/, '').trim()
}
