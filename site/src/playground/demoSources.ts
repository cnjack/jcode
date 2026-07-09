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
}
