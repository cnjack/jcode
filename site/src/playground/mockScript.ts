/**
 * Mock script for the ChatDemo playground component.
 *
 * A scripted conversation that streams through message → tool call (terminal) →
 * tool call (edit/diff) → approval → assistant text. This showcases every
 * ThreadItem kind + the shimmer/tool-renderer machinery without a backend.
 *
 * Drives a MockRuntime: each step is pushed/appendText'd on a timer, mimicking
 * a real agent run. Exported so the docs page and any external demo can reuse it.
 */

import { createMockRuntime } from 'jcode-ui-core/runtime'
import type { ChatRuntime } from 'jcode-ui-core/runtime'
import type { ThreadItem, Message, ToolCall, Approval } from 'jcode-ui-core'

export interface ScriptStep {
  /** Delay (ms) before this step fires, relative to the previous. */
  delay: number
  /** What to do. */
  do: (rt: ScriptRuntime) => void
}

export type ScriptRuntime = ReturnType<typeof createMockRuntime> & ChatRuntime

/**
 * Run a script against a mock runtime. Returns a cancel function (clears
 * pending timeouts so React can unmount cleanly).
 */
export function runScript(rt: ScriptRuntime, steps: ScriptStep[]): () => void {
  const timers: ReturnType<typeof setTimeout>[] = []
  let acc = 0
  for (const step of steps) {
    acc += step.delay
    timers.push(setTimeout(() => step.do(rt), acc))
  }
  return () => timers.forEach(clearTimeout)
}

// Helpers that build correctly-typed ThreadItems (TS can't narrow a generic
// ctor over a discriminated union, so we use one builder per kind).
let seq = 0
const msg = (data: Message): ThreadItem => ({ kind: 'message', data, seq: ++seq })
const tool = (data: ToolCall): ThreadItem => ({ kind: 'tool', data, seq: ++seq })
const approval = (data: Approval): ThreadItem => ({ kind: 'approval', data, seq: ++seq })

/** Resolve a running tool by id (set status + output). Returns a new items array. */
function resolveTool(items: ThreadItem[], id: string, patch: Partial<ToolCall>): ThreadItem[] {
  return items.map((i) =>
    i.kind === 'tool' && i.data.id === id ? { ...i, data: { ...i.data, ...patch } } : i,
  )
}
function resolveApprovalItem(items: ThreadItem[], id: string, patch: Partial<Approval>): ThreadItem[] {
  return items.map((i) =>
    i.kind === 'approval' && i.data.id === id ? { ...i, data: { ...i.data, ...patch } } : i,
  )
}

/** The canned demo conversation used on the website footprint + docs page. */
export function buildDemoScript(): ScriptStep[] {
  // Reset the seq counter so each run starts clean.
  seq = 0
  return [
    {
      delay: 300,
      do: (rt) =>
        rt.push(
          msg({
            id: 'u1',
            role: 'user',
            content: 'Fix the goroutine leak in server.go and verify with tests.',
            timestamp: Date.now(),
          }),
        ),
    },
    { delay: 500, do: (rt) => rt.setRunning(true) },
    { delay: 200, do: (rt) => rt.appendText("I'll read the file first to find the leak.") },
    {
      delay: 600,
      do: (rt) =>
        rt.push(
          tool({
            id: 't1',
            name: 'read',
            args: JSON.stringify({ path: 'server.go' }),
            status: 'running',
            timestamp: Date.now(),
            displayInfo: { title: 'read', subtitle: 'server.go', icon: 'file', category: 'context' },
          }),
        ),
    },
    {
      delay: 700,
      do: (rt) =>
        rt.setItems(
          resolveTool(rt.getState().items, 't1', {
            status: 'done',
            output:
              '   1│package main\n   2│\n   3│func handle(conn Conn) { go process(conn) }\n   4│func serve() { for { handle(accept()) } }',
          }),
        ),
    },
    {
      delay: 400,
      do: (rt) =>
        rt.appendText(
          'Found it — `handle()` spawns `process(conn)` with `go` but never waits. I\'ll add a WaitGroup.',
        ),
    },
    {
      delay: 600,
      do: (rt) =>
        rt.push(
          tool({
            id: 't2',
            name: 'edit',
            args: JSON.stringify({
              path: 'server.go',
              old_string: 'func handle(conn Conn) { go process(conn) }',
              new_string:
                'var wg sync.WaitGroup\n\nfunc handle(conn Conn) {\n\twg.Add(1)\n\tgo func() { defer wg.Done(); process(conn) }()\n}',
            }),
            status: 'running',
            timestamp: Date.now(),
            displayInfo: { title: 'edit', subtitle: 'server.go', icon: 'pencil', category: 'mutation' },
          }),
        ),
    },
    { delay: 900, do: (rt) => rt.setItems(resolveTool(rt.getState().items, 't2', { status: 'done' })) },
    {
      delay: 400,
      do: (rt) =>
        rt.push(
          approval({
            id: 'a1',
            tool_name: 'execute',
            tool_args: JSON.stringify({ command: 'go test ./...' }),
            is_external: false,
          }),
        ),
    },
    {
      delay: 2500,
      do: (rt) => rt.setItems(resolveApprovalItem(rt.getState().items, 'a1', { resolved: true, approved: true })),
    },
    {
      delay: 300,
      do: (rt) =>
        rt.push(
          tool({
            id: 't3',
            name: 'execute',
            args: JSON.stringify({ command: 'go test ./...' }),
            status: 'running',
            timestamp: Date.now(),
            displayInfo: { title: 'execute', subtitle: 'go test ./...', icon: 'terminal', category: 'execution' },
          }),
        ),
    },
    {
      delay: 1100,
      do: (rt) =>
        rt.setItems(
          resolveTool(rt.getState().items, 't3', {
            status: 'done',
            output: 'ok  net/server  0.41s\nok  net/client  0.28s\nPASS',
          }),
        ),
    },
    {
      delay: 300,
      do: (rt) =>
        rt.appendText(
          '\n\n**Leak fixed.** All tests pass — the goroutine is now tracked by a `sync.WaitGroup` and joined on shutdown.',
        ),
    },
    { delay: 600, do: (rt) => rt.setRunning(false) },
    // Showcase the Reasoning + Sources components on the final assistant message.
    {
      delay: 400,
      do: (rt) => {
        const items = rt.getState().items
        // Build a NEW items array with a NEW data object for the last assistant
        // message (immutable update) so React.memo re-renders it.
        let updated = false
        const next = items.map((it) => {
          if (!updated && it.kind === 'message' && it.data.role === 'assistant') {
            updated = true
            return {
              ...it,
              data: {
                ...it.data,
                reasoning:
                  'The leak is in handle() — it spawns process(conn) via `go` without tracking it. ' +
                  'A sync.WaitGroup lets the server join all handlers on shutdown. The test failure ' +
                  'confirms the goroutine escapes; adding wg.Done() closes the lifecycle.',
                sources: [
                  { id: 's1', title: 'Go concurrency: WaitGroups', url: 'https://go.dev/ref/spec#Go_statements', snippet: 'A WaitGroup waits for a collection of goroutines to finish.' },
                  { id: 's2', title: 'server.go:42', snippet: 'func handle(conn Conn) { go process(conn) }' },
                ],
              },
            }
          }
          return it
        })
        if (updated) rt.setItems(next)
      },
    },
  ]
}
