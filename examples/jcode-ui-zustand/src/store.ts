/**
 * Host chat store — Zustand + jcode-ui ThreadItem timeline.
 *
 * This is the production integration pattern: own the state, project it through
 * createExternalStoreRuntime, bind actions that mutate + (optionally) hit an API.
 */

import { create } from 'zustand'
import type { ThreadItem, Message, ToolCall } from 'jcode-ui-core'

export interface ChatState {
  items: ThreadItem[]
  isRunning: boolean
  seq: number
  // mutations
  appendUser: (text: string) => void
  appendAssistant: (text: string) => void
  appendTool: (tool: ToolCall) => void
  setRunning: (v: boolean) => void
  stop: () => void
  resolveApproval: (id: string, approved: boolean, approveAll?: boolean) => void
  submitAskUser: (id: string, answers: { question_header: string; answer: string; selected?: string[] }[]) => void
  editMessage: (id: string, newText: string) => void
}

function nextSeq(get: () => ChatState): number {
  const seq = get().seq + 1
  return seq
}

export const useChatStore = create<ChatState>((set, get) => ({
  items: [],
  isRunning: false,
  seq: 0,

  appendUser: (text) => {
    const seq = nextSeq(get)
    const msg: Message = {
      id: `u_${seq}`,
      role: 'user',
      content: text,
      timestamp: Date.now(),
    }
    set({
      seq,
      items: [...get().items, { kind: 'message', data: msg, seq }],
    })
  },

  appendAssistant: (text) => {
    const seq = nextSeq(get)
    const msg: Message = {
      id: `a_${seq}`,
      role: 'assistant',
      content: text,
      timestamp: Date.now(),
      durationMs: 600,
    }
    set({
      seq,
      items: [...get().items, { kind: 'message', data: msg, seq }],
      isRunning: false,
    })
  },

  appendTool: (tool) => {
    const seq = nextSeq(get)
    set({
      seq,
      items: [...get().items, { kind: 'tool', data: { ...tool, id: tool.id || `t_${seq}` }, seq }],
    })
  },

  setRunning: (v) => set({ isRunning: v }),

  stop: () => set({ isRunning: false }),

  resolveApproval: (id, approved) => {
    set({
      items: get().items.map((i) =>
        i.kind === 'approval' && i.data.id === id
          ? { ...i, data: { ...i.data, resolved: true, approved } }
          : i,
      ),
    })
  },

  submitAskUser: (id, answers) => {
    set({
      items: get().items.map((i) =>
        i.kind === 'tool' && (i.data.askUserId === id || i.data.id === id)
          ? {
              ...i,
              data: {
                ...i.data,
                status: 'done',
                askUserId: undefined,
                askUserQuestions: undefined,
                output: JSON.stringify(answers),
              },
            }
          : i,
      ),
    })
  },

  editMessage: (id, newText) => {
    set({
      items: get().items.map((i) =>
        i.kind === 'message' && i.data.id === id
          ? { ...i, data: { ...i.data, content: newText } }
          : i,
      ),
    })
  },
}))

/** Seed a short conversation for the demo. */
export function seedDemo() {
  const s = useChatStore.getState()
  if (s.items.length > 0) return
  s.appendUser('Wire jcode-ui to Zustand.')
  s.appendTool({
    id: 't1',
    name: 'read',
    args: JSON.stringify({ path: 'store.ts' }),
    status: 'done',
    timestamp: Date.now(),
    output: '   1│export const useChatStore = create(…)',
    displayInfo: { title: 'read', subtitle: 'store.ts' },
  })
  s.appendAssistant(
    'Use `createExternalStoreRuntime({ store: useChatStore, select, actions })`. The store owns the timeline; the UI only renders it.',
  )
}
