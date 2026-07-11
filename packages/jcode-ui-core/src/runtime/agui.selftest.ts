/**
 * Self-test for the AG-UI runtime adapter. No test runner in this repo, so this
 * is a plain script: replay scripted event streams through an injected transport
 * and assert the resulting RuntimeState shape.
 *
 * Deliberately imports nothing from `node:*` (only `console` + `setTimeout`,
 * both in the DOM lib) so it typechecks under the package's config without
 * pulling in `@types/node`. Run the COMPILED output — the source uses `.js` ESM
 * specifiers that `--experimental-strip-types` won't resolve to `.ts`:
 *
 *   pnpm build && node dist/runtime/agui.selftest.js
 */

import type { Message, ThreadItem, ToolCall } from '../types/index.js'
import type { AGUIEvent, AGUIRunInput, AGUITransport } from './agui.js'
import { createAGUIRuntime, parseSSEStream } from './agui.js'

function assert(cond: boolean, msg: string): void {
  if (!cond) throw new Error(`ASSERT FAILED: ${msg}`)
}
function eq(actual: unknown, expected: unknown, msg: string): void {
  const a = JSON.stringify(actual)
  const b = JSON.stringify(expected)
  if (a !== b) throw new Error(`ASSERT FAILED: ${msg} (got ${a}, want ${b})`)
}
function msgAt(items: ThreadItem[], i: number): Message {
  const it = items[i]
  if (it.kind !== 'message') throw new Error(`expected message at [${i}], got ${it?.kind}`)
  return it.data
}
function toolAt(items: ThreadItem[], i: number): ToolCall {
  const it = items[i]
  if (it.kind !== 'tool') throw new Error(`expected tool at [${i}], got ${it?.kind}`)
  return it.data
}

/** Drain microtasks + the batched flush until the run settles. */
async function settle(rt: { getState: () => { isRunning: boolean } }): Promise<void> {
  for (let i = 0; i < 50; i++) {
    await new Promise<void>((r) => setTimeout(r, 0))
    if (!rt.getState().isRunning) break
  }
  await new Promise<void>((r) => setTimeout(r, 0))
}

/** A transport that replays a fixed script and records the run input it saw. */
function scriptedTransport(script: AGUIEvent[]): {
  transport: AGUITransport
  seen: { input: AGUIRunInput | null }
} {
  const seen: { input: AGUIRunInput | null } = { input: null }
  const transport: AGUITransport = async function* (input, _signal) {
    seen.input = input
    for (const ev of script) yield ev
  }
  return { transport, seen }
}

async function testStreamingRun(): Promise<void> {
  const script: AGUIEvent[] = [
    { type: 'RUN_STARTED', threadId: 't', runId: 'r' },
    { type: 'REASONING_MESSAGE_CONTENT', delta: 'thinking…' },
    { type: 'TEXT_MESSAGE_START', messageId: 'm1', role: 'assistant' },
    { type: 'TEXT_MESSAGE_CONTENT', messageId: 'm1', delta: 'Hello ' },
    { type: 'TEXT_MESSAGE_CONTENT', messageId: 'm1', delta: 'world' },
    { type: 'TEXT_MESSAGE_END', messageId: 'm1' },
    { type: 'TOOL_CALL_START', toolCallId: 'tc1', toolCallName: 'search' },
    { type: 'TOOL_CALL_ARGS', toolCallId: 'tc1', delta: '{"q":' },
    { type: 'TOOL_CALL_ARGS', toolCallId: 'tc1', delta: '"cats"}' },
    { type: 'TOOL_CALL_END', toolCallId: 'tc1' },
    { type: 'TOOL_CALL_RESULT', toolCallId: 'tc1', content: { hits: 3 } },
    { type: 'STATE_SNAPSHOT', snapshot: { counter: 1, nested: { a: 'x' } } },
    {
      type: 'STATE_DELTA',
      delta: [
        { op: 'replace', path: '/counter', value: 2 },
        { op: 'add', path: '/nested/b', value: 'y' },
        { op: 'remove', path: '/nested/a' },
      ],
    },
    { type: 'RUN_FINISHED' },
  ]
  const { transport, seen } = scriptedTransport(script)
  const rt = createAGUIRuntime({ url: 'http://test', transport })

  let notifications = 0
  rt.subscribe(() => {
    notifications++
  })

  rt.actions.sendMessage('hi')
  await settle(rt)

  const { items, isRunning, connection } = rt.getState()
  eq(isRunning, false, 'run should be finished')
  eq(connection, 'connected', 'healthy run stays connected')
  eq(items.length, 3, 'user + assistant + tool')

  const user = msgAt(items, 0)
  eq(user.role, 'user', 'items[0] is the user message')
  eq(user.content, 'hi', 'user text preserved')

  const assistant = msgAt(items, 1)
  eq(assistant.role, 'assistant', 'items[1] is assistant')
  eq(assistant.content, 'Hello world', 'streamed text accumulated')
  eq(assistant.reasoning, 'thinking…', 'buffered reasoning attached to assistant')

  const tool = toolAt(items, 2)
  eq(tool.name, 'search', 'tool name from TOOL_CALL_START')
  eq(tool.args, '{"q":"cats"}', 'tool args concatenated raw')
  eq(tool.status, 'done', 'tool done after END/RESULT')
  eq(tool.output, '{"hits":3}', 'tool result serialized to JSON string')

  eq(rt.getAgentState(), { counter: 2, nested: { b: 'y' } }, 'STATE snapshot + RFC6902 delta')

  assert(seen.input !== null, 'transport received run input')
  eq(seen.input?.messages.length, 1, 'run input carries the user message')
  eq(seen.input?.messages[0]?.content, 'hi', 'run input user content')
  assert(notifications > 0, 'subscribers were notified')
  console.log('  ok  streaming run → items/tool/reasoning/agentState')
}

async function testMessagesSnapshot(): Promise<void> {
  const script: AGUIEvent[] = [
    { type: 'RUN_STARTED' },
    {
      type: 'MESSAGES_SNAPSHOT',
      messages: [
        { id: 'u1', role: 'user', content: 'hi there' },
        {
          id: 'a1',
          role: 'assistant',
          content: '',
          toolCalls: [{ id: 'call1', type: 'function', function: { name: 'lookup', arguments: '{"x":1}' } }],
        },
        { id: 't1', role: 'tool', toolCallId: 'call1', name: 'lookup', content: '{"ok":true}' },
        { id: 'a2', role: 'assistant', content: 'done' },
      ],
    },
    { type: 'RUN_FINISHED' },
  ]
  const { transport } = scriptedTransport(script)
  const rt = createAGUIRuntime({ url: 'http://test', transport })
  rt.actions.sendMessage('ignored — snapshot rebuilds the timeline')
  await settle(rt)

  const { items } = rt.getState()
  eq(items.length, 3, 'empty assistant shell collapsed; 3 items remain')
  eq(msgAt(items, 0).content, 'hi there', 'snapshot user message')
  const tool = toolAt(items, 1)
  eq(tool.name, 'lookup', 'assistant.toolCalls → ToolCall item')
  eq(tool.args, '{"x":1}', 'tool args from function.arguments')
  eq(tool.output, '{"ok":true}', 'tool-role message attached as output')
  eq(tool.status, 'done', 'snapshot tools are terminal')
  eq(msgAt(items, 2).content, 'done', 'trailing assistant message')
  console.log('  ok  MESSAGES_SNAPSHOT rebuild → tool-call correlation')
}

async function testRunError(): Promise<void> {
  const script: AGUIEvent[] = [
    { type: 'RUN_STARTED' },
    { type: 'RUN_ERROR', message: 'boom', code: 'E42' },
  ]
  const { transport } = scriptedTransport(script)
  const rt = createAGUIRuntime({ url: 'http://test', transport })
  rt.actions.sendMessage('go')
  await settle(rt)
  const { items, isRunning } = rt.getState()
  eq(isRunning, false, 'error ends the run')
  const err = msgAt(items, items.length - 1)
  eq(err.role, 'system', 'error is a system message')
  eq(err.level, 'error', 'error level set')
  eq(err.content, 'boom', 'error message text')
  eq(err.detail, 'E42', 'error code in detail')
  console.log('  ok  RUN_ERROR → system error message')
}

async function testStateDeltaFromEmpty(): Promise<void> {
  const script: AGUIEvent[] = [
    { type: 'RUN_STARTED' },
    { type: 'STATE_DELTA', delta: [{ op: 'add', path: '/x', value: 1 }] },
    { type: 'RUN_FINISHED' },
  ]
  const { transport } = scriptedTransport(script)
  const rt = createAGUIRuntime({ url: 'http://test', transport })
  rt.actions.sendMessage('go')
  await settle(rt)
  eq(rt.getAgentState(), { x: 1 }, 'STATE_DELTA with no prior snapshot starts from {}')
  console.log('  ok  STATE_DELTA before snapshot')
}

async function testTransportFailure(): Promise<void> {
  const transport: AGUITransport = async function* (_input, _signal) {
    // A generator that emits nothing and fails, like a dropped connection.
    throw new Error('network down')
  }
  const rt = createAGUIRuntime({ url: 'http://test', transport })
  rt.actions.sendMessage('go')
  await settle(rt)
  const { items, isRunning, connection } = rt.getState()
  eq(isRunning, false, 'failed run ends')
  eq(connection, 'disconnected', 'transport failure marks disconnected')
  const err = msgAt(items, items.length - 1)
  eq(err.level, 'error', 'transport error surfaced as system error')
  eq(err.content, 'network down', 'error message propagated')
  console.log('  ok  transport failure → disconnected + error message')
}

async function testSSEParser(): Promise<void> {
  const frames =
    'data: {"type":"RUN_STARTED"}\n\n' +
    ': keep-alive\n\n' +
    'data: {"type":"TEXT_MESSAGE_CONTENT",' + // split mid-frame across chunks below
    '"messageId":"m","delta":"hi"}\n\n' +
    'data: [DONE]\n\n'
  const bytes = new TextEncoder().encode(frames)
  const mid = 40
  const stream = new ReadableStream<Uint8Array>({
    start(c) {
      c.enqueue(bytes.slice(0, mid))
      c.enqueue(bytes.slice(mid))
      c.close()
    },
  })
  const out: AGUIEvent[] = []
  for await (const ev of parseSSEStream(stream)) out.push(ev)
  eq(out.length, 2, 'comment + [DONE] filtered; 2 real events')
  eq(out[0]?.type, 'RUN_STARTED', 'first SSE event')
  eq(out[1]?.type, 'TEXT_MESSAGE_CONTENT', 'second SSE event survives chunk split')
  console.log('  ok  SSE parser (chunked, comments, [DONE])')
}

async function main(): Promise<void> {
  console.log('agui.selftest:')
  await testStreamingRun()
  await testMessagesSnapshot()
  await testRunError()
  await testStateDeltaFromEmpty()
  await testTransportFailure()
  await testSSEParser()
  console.log('ALL PASS')
}

main().catch((err) => {
  console.error(err instanceof Error ? err.stack ?? err.message : String(err))
  // Non-zero exit for CI without importing node:process.
  const g = globalThis as { process?: { exitCode?: number } }
  if (g.process) g.process.exitCode = 1
})
