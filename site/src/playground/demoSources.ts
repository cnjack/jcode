/**
 * Source snippets shown in the Code tab of ComponentDemo.
 * Keep these short and copy-pasteable — they mirror the live fixtures, not the
 * full demo harness.
 */

export const DEMO_SOURCES: Record<string, string> = {
  thread: `import {
  RuntimeProvider,
  ToolRegistryProvider,
  createDefaultToolRegistry,
  createMockRuntime,
  Thread,
  ChatInput,
} from 'jcode-ui'
import 'jcode-ui/styles.css'

const runtime = createMockRuntime({
  items: [
    {
      kind: 'message',
      seq: 1,
      data: {
        id: 'u1',
        role: 'user',
        content: 'Fix the race in server.go',
        timestamp: Date.now(),
      },
    },
    {
      kind: 'message',
      seq: 2,
      data: {
        id: 'a1',
        role: 'assistant',
        content: 'Done — tests are green.',
        timestamp: Date.now(),
        durationMs: 4200,
      },
    },
  ],
})

export function App() {
  return (
    <RuntimeProvider runtime={runtime}>
      <ToolRegistryProvider registry={createDefaultToolRegistry()}>
        <div style={{ height: 420, display: 'flex', flexDirection: 'column' }}>
          <div style={{ flex: 1, minHeight: 0 }}>
            <Thread virtualize={false} />
          </div>
          <ChatInput showContextBar />
        </div>
      </ToolRegistryProvider>
    </RuntimeProvider>
  )
}`,

  message: `import { RuntimeProvider, createMockRuntime, Message } from 'jcode-ui'
import 'jcode-ui/styles.css'

const runtime = createMockRuntime()

const user = {
  id: 'u1',
  role: 'user' as const,
  content: 'Fix the race condition in \`server.go\`.',
  timestamp: Date.now(),
}

const assistant = {
  id: 'a1',
  role: 'assistant' as const,
  content: "Here's the fix with a mutex around the map write.",
  timestamp: Date.now(),
  durationMs: 4200,
  reasoning: 'Concurrent map writes without synchronization…',
  sources: [
    { id: 's1', title: 'Go memory model', url: 'https://go.dev/ref/mem' },
  ],
}

export function Demo() {
  return (
    <RuntimeProvider runtime={runtime}>
      <Message message={user} canEdit />
      <Message message={assistant} />
    </RuntimeProvider>
  )
}`,

  'chat-input': `import {
  RuntimeProvider,
  createMockRuntime,
  ChatInput,
} from 'jcode-ui'
import 'jcode-ui/styles.css'

const runtime = createMockRuntime({
  state: {
    tokenSnapshot: {
      total_tokens: 98000,
      prompt_tokens: 92000,
      completion_tokens: 6000,
      model_context_limit: 128000,
    },
  },
})

export function Demo() {
  return (
    <RuntimeProvider runtime={runtime}>
      <ChatInput
        slashCommands={[
          { slash: '/goal', description: 'Set the session goal' },
          { slash: '/compact', description: 'Compact context' },
        ]}
        allowImages
        showContextBar
        placeholder="Send a message… try /"
      />
    </RuntimeProvider>
  )
}`,

  'tool-call': `import {
  RuntimeProvider,
  ToolRegistryProvider,
  createDefaultToolRegistry,
  createMockRuntime,
  ToolCallCard,
} from 'jcode-ui'
import 'jcode-ui/styles.css'

const tool = {
  id: 't1',
  name: 'execute',
  args: JSON.stringify({ command: 'go test ./...' }),
  status: 'done' as const,
  timestamp: Date.now(),
  output: 'ok\\tgithub.com/acme/server\\t0.412s\\nPASS',
  displayInfo: { title: 'execute', subtitle: 'go test ./...' },
}

export function Demo() {
  return (
    <RuntimeProvider runtime={createMockRuntime()}>
      <ToolRegistryProvider registry={createDefaultToolRegistry()}>
        <ToolCallCard tool={tool} />
      </ToolRegistryProvider>
    </RuntimeProvider>
  )
}`,

  'tool-gallery': `import {
  RuntimeProvider,
  ToolRegistryProvider,
  createDefaultToolRegistry,
  createMockRuntime,
  ToolCallCard,
} from 'jcode-ui'
import 'jcode-ui/styles.css'

// Each tool.name maps to a default renderer:
// execute → terminal | read → file-viewer | edit → diff | grep → search
// todowrite → todo | load_skill → skill | team_list → team | * → generic

const tools = [
  { id: '1', name: 'execute', args: '{"command":"go test"}', status: 'done', timestamp: 0, output: 'PASS' },
  { id: '2', name: 'edit', args: '{"path":"server.go"}', status: 'done', timestamp: 0, output: '- old\\n+ new' },
]

export function Demo() {
  const registry = createDefaultToolRegistry()
  return (
    <RuntimeProvider runtime={createMockRuntime()}>
      <ToolRegistryProvider registry={registry}>
        {tools.map((t) => (
          <ToolCallCard key={t.id} tool={t as never} />
        ))}
      </ToolRegistryProvider>
    </RuntimeProvider>
  )
}`,

  approval: `import {
  RuntimeProvider,
  createMockRuntime,
  ApprovalBanner,
} from 'jcode-ui'
import 'jcode-ui/styles.css'

const pending = {
  id: 'ap1',
  tool_name: 'execute',
  tool_args: JSON.stringify({ command: 'rm -rf ./build' }),
  is_external: false,
}

const resolved = { ...pending, id: 'ap2', resolved: true, approved: true }

export function Demo() {
  // MockRuntime default resolveApproval updates items for interactive demos.
  return (
    <RuntimeProvider runtime={createMockRuntime()}>
      <ApprovalBanner approval={pending} />
      <ApprovalBanner approval={resolved} />
    </RuntimeProvider>
  )
}`,

  'ask-user': `import {
  RuntimeProvider,
  createMockRuntime,
  AskUserCard,
} from 'jcode-ui'
import 'jcode-ui/styles.css'

const tool = {
  id: 'ask1',
  name: 'ask_user',
  args: '{}',
  status: 'running' as const,
  timestamp: Date.now(),
  askUserId: 'ask1',
  askUserQuestions: [
    {
      question: 'Which test strategy should I use?',
      header: 'test strategy',
      options: [
        { label: 'Unit only', description: 'Fast' },
        { label: 'Integration', description: 'Real DB' },
        { label: 'Both' },
      ],
    },
  ],
}

export function Demo() {
  return (
    <RuntimeProvider runtime={createMockRuntime()}>
      <AskUserCard tool={tool} />
    </RuntimeProvider>
  )
}`,

  reasoning: `import { Reasoning } from 'jcode-ui'
import 'jcode-ui/styles.css'

export function Demo() {
  return (
    <Reasoning
      reasoning="The panic stack points at concurrent map writes…"
      durationMs={3200}
      defaultExpanded
    />
  )
}`,

  sources: `import { Sources } from 'jcode-ui'
import 'jcode-ui/styles.css'

export function Demo() {
  return (
    <Sources
      sources={[
        {
          id: 's1',
          title: 'Go memory model',
          url: 'https://go.dev/ref/mem',
          snippet: 'Happens-before rules for mutexes.',
        },
        { id: 's2', title: 'server.go:48', snippet: 'go process(req)' },
      ]}
    />
  )
}`,

  attachment: `import { AttachmentList, ChatInput } from 'jcode-ui'
import 'jcode-ui/styles.css'

// Standalone tiles (composer / custom UIs)
export function Tiles() {
  return (
    <AttachmentList
      images={[{ data: '<base64…>', media_type: 'image/png', name: 'shot.png' }]}
      onRemove={(i) => console.log('remove', i)}
      size={56}
    />
  )
}

// Preferred: ChatInput owns paste + paperclip + strip
export function Composer() {
  return <ChatInput allowImages placeholder="Paste or attach an image…" />
}`,

  'context-bar': `import {
  RuntimeProvider,
  createMockRuntime,
  ContextBar,
} from 'jcode-ui'
import 'jcode-ui/styles.css'

const runtime = createMockRuntime({
  state: {
    tokenSnapshot: {
      total_tokens: 98000,
      prompt_tokens: 92000,
      completion_tokens: 6000,
      model_context_limit: 128000,
    },
  },
})

export function Demo() {
  return (
    <RuntimeProvider runtime={runtime}>
      <ContextBar
        size={28}
        breakdown={{
          context_limit: 128000,
          system_prompt_tokens: 1200,
          system_tools_tokens: 8400,
          mcp_tools_tokens: 2100,
          skills_tokens: 900,
          messages_tokens: 85400,
        }}
      />
    </RuntimeProvider>
  )
}`,

  theming: `/* Override jcode-ui tokens in your global CSS */

:root {
  --color-primary: #6366f1;
  --color-background: #fafaf9;
  --radius-lg: 10px;
}

.dark {
  --color-primary: #818cf8;
  --color-background: #0a0a0a;
}

/* Toggle dark mode */
// document.documentElement.classList.toggle('dark')`,

  welcome: `import {
  RuntimeProvider,
  createMockRuntime,
  Thread,
  ThreadWelcome,
  Suggestions,
} from 'jcode-ui'
import 'jcode-ui/styles.css'

const runtime = createMockRuntime()

// Drop the hero into Thread's emptyState; Suggestions send through the runtime.
const welcome = (
  <ThreadWelcome
    title="What can I help you ship?"
    subtitle="Ask about your codebase, or pick a starter below."
  >
    <Suggestions
      items={[
        { id: 's1', label: 'Explain this repo', prompt: 'Give me a tour of this repository.' },
        { id: 's2', label: 'Find the race condition', prompt: 'Fix the race in server.go.' },
        { id: 's3', label: 'Write tests', prompt: 'Add table-driven tests for the parser.' },
      ]}
    />
  </ThreadWelcome>
)

export function Demo() {
  return (
    <RuntimeProvider runtime={runtime}>
      <Thread emptyState={welcome} />
    </RuntimeProvider>
  )
}`,

  branching: `import { RuntimeProvider, createMockRuntime, Message } from 'jcode-ui'
import 'jcode-ui/styles.css'

// content mirrors the active version; BranchPicker steps activeVersionId.
const assistant = {
  id: 'a1',
  role: 'assistant' as const,
  content: 'Use sync.Map for the shared registry — lock-free reads.',
  timestamp: Date.now(),
  durationMs: 3800,
  activeVersionId: 'v2',
  versions: [
    { id: 'v1', content: 'Wrap map access in a sync.Mutex.', timestamp: Date.now() },
    { id: 'v2', content: 'Use sync.Map for the shared registry.', timestamp: Date.now() },
    { id: 'v3', content: 'Shard the map by key hash.', timestamp: Date.now() },
  ],
}

export function Demo() {
  // MockRuntime's default switchVersion + submitFeedback make the controls live.
  return (
    <RuntimeProvider runtime={createMockRuntime({ items: [{ kind: 'message', seq: 1, data: assistant }] })}>
      <Message message={assistant} />
    </RuntimeProvider>
  )
}`,

  connection: `import {
  RuntimeProvider,
  createMockRuntime,
  ConnectionBanner,
} from 'jcode-ui'
import 'jcode-ui/styles.css'

const runtime = createMockRuntime({ state: { connection: 'reconnecting' } })

export function Demo() {
  // ConnectionBanner reads state.connection; nothing renders while connected.
  return (
    <RuntimeProvider runtime={runtime}>
      <ConnectionBanner />
      <button onClick={() => runtime.patchState({ connection: 'disconnected' })}>
        Drop
      </button>
      <button onClick={() => runtime.patchState({ connection: 'connected' })}>
        Restore
      </button>
    </RuntimeProvider>
  )
}`,

  tasklist: `import { TaskList } from 'jcode-ui'
import 'jcode-ui/styles.css'

const items = [
  { id: 1, title: 'Read the current parser', status: 'completed' as const },
  { id: 2, title: 'Extract the tokenizer', status: 'in_progress' as const },
  { id: 3, title: 'Add table-driven tests', status: 'pending' as const },
  { id: 4, title: 'Delete the legacy shim', status: 'cancelled' as const },
]

export function Demo() {
  return <TaskList title="Ship the parser refactor" items={items} />
}`,

  'model-selector': `import { useState } from 'react'
import { ChatInput, ModelSelector } from 'jcode-ui'
import 'jcode-ui/styles.css'

const models = [
  { id: 'claude-opus-4', label: 'Claude Opus 4', provider: 'Anthropic', description: 'Most capable' },
  { id: 'claude-sonnet-4', label: 'Claude Sonnet 4', provider: 'Anthropic' },
  { id: 'gpt-5', label: 'GPT-5', provider: 'OpenAI' },
  { id: 'gemini-2.5-pro', label: 'Gemini 2.5 Pro', provider: 'Google' },
]

export function Demo() {
  const [model, setModel] = useState('claude-opus-4')
  // Typically the composer's leadingControls:
  return (
    <ChatInput
      leadingControls={<ModelSelector models={models} value={model} onChange={setModel} />}
    />
  )
}`,

  artifact: `import { Artifact } from 'jcode-ui'
import 'jcode-ui/styles.css'

export function Demo() {
  return (
    <Artifact
      title="vite.config.ts"
      subtitle="7 lines · typescript"
      actions={<button type="button" onClick={() => copy(source)}>Copy</button>}
      onClose={() => setOpen(false)}
    >
      <pre style={{ margin: 0, padding: '0.75rem' }}>{source}</pre>
    </Artifact>
  )
}`,

  'thread-list': `import {
  createMockThreadStore,
  ThreadStoreProvider,
} from 'jcode-ui-core'
import { ThreadList } from 'jcode-ui'
import 'jcode-ui/styles.css'

const store = createMockThreadStore({
  activeId: 'th1',
  threads: [
    { id: 'th1', title: 'Refactor auth middleware', updatedAt: Date.now(), status: 'running' },
    { id: 'th2', title: 'Fix flaky payment test', updatedAt: Date.now() - 6e5 },
    { id: 'th3', title: 'Draft the release notes', updatedAt: Date.now() - 5e6 },
    { id: 'th4', title: 'Investigate memory leak', updatedAt: Date.now() - 3e8, archived: true },
  ],
})

export function Demo() {
  // Controls (New / rename / archive / delete) render only for wired actions.
  return (
    <ThreadStoreProvider store={store}>
      <ThreadList title="Sessions" />
    </ThreadStoreProvider>
  )
}`,

  'tool-gallery-2': `import {
  RuntimeProvider,
  ToolRegistryProvider,
  createDefaultToolRegistry,
  createMockRuntime,
  ToolCallCard,
} from 'jcode-ui'
import { TestResultsRenderer, StackTraceRenderer } from 'jcode-ui/tool-renderers'
import 'jcode-ui/styles.css'

// FileTree ships in the default registry (list_dir / glob). Map the two code
// renderers onto the tool names your host emits (here: go_test, panic).
const registry = createDefaultToolRegistry()
registry.register('go_test', TestResultsRenderer)
registry.register('panic', StackTraceRenderer)

const goTest = {
  id: '1', name: 'go_test', args: '{}', status: 'error' as const, timestamp: 0,
  output: '--- FAIL: TestSubtraction (0.01s)\\n    math_test.go:24: got 1; want 2\\nFAIL',
}

export function Demo() {
  return (
    <RuntimeProvider runtime={createMockRuntime()}>
      <ToolRegistryProvider registry={registry}>
        <ToolCallCard tool={goTest} />
      </ToolRegistryProvider>
    </RuntimeProvider>
  )
}`,
}
