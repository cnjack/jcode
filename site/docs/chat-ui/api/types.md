---
title: Types
parent: API Reference
nav_order: 1
---

# Types

Core types from `jcode-ui-core` (re-exported from `jcode-ui` where noted).

```ts
import type {
  Message,
  ToolCall,
  Approval,
  ThreadItem,
  TokenSnapshot,
  TaskContextBreakdown,
  Goal,
  TodoItem,
  Role,
} from 'jcode-ui-core'
// or from 'jcode-ui' (Message is exported as MessageData type alias)
```

## Message

```ts
type Role = 'user' | 'assistant' | 'system'
type SystemLevel = 'error' | 'notice'

interface ChatImage {
  data: string       // base64, no data: prefix
  media_type: string
}

interface MessageSource {
  id: string
  title: string
  url?: string
  snippet?: string
}

interface Message {
  id: string
  role: Role
  content: string
  timestamp: number
  source?: string           // e.g. 'wechat'
  images?: ChatImage[]
  level?: SystemLevel       // system messages
  detail?: string
  durationMs?: number       // assistant turn elapsed
  reasoning?: string        // chain-of-thought
  sources?: MessageSource[]
}
```

## ToolCall

```ts
type ToolStatus = 'running' | 'done' | 'error'

interface ToolDisplayInfo {
  title: string
  subtitle?: string
  icon?: string
  category?: string         // 'context' | 'mutation' | 'execution' | …
}

interface ToolCall {
  id: string
  toolCallID?: string
  name: string
  args: string              // raw JSON string
  output?: string
  displayOutput?: string
  error?: string
  status: ToolStatus
  timestamp: number
  displayInfo?: ToolDisplayInfo
  children?: ToolCall[]     // subagent nesting
  askUserId?: string
  askUserQuestions?: AskUserQuestion[]
}
```

## Ask user

```ts
interface AskUserOption {
  label: string
  description?: string
}

interface AskUserQuestion {
  question: string
  header?: string
  options?: AskUserOption[]
  multi_select?: boolean
}

interface AskUserAnswer {
  question_header: string
  answer: string
  selected?: string[]
}
```

## Approval

```ts
interface Approval {
  id: string
  tool_name: string
  tool_args: string
  is_external: boolean
  resolved?: boolean
  approved?: boolean
  resolving?: boolean
}
```

## ThreadItem

```ts
type ThreadItem =
  | { kind: 'message'; data: Message; seq: number }
  | { kind: 'tool'; data: ToolCall; seq: number }
  | { kind: 'approval'; data: Approval; seq: number }

function isMessageItem(i: ThreadItem): boolean
function isToolItem(i: ThreadItem): boolean
function isApprovalItem(i: ThreadItem): boolean
```

`seq` is the stable React / virtualizer key across streaming updates.

## Tokens & context

```ts
interface TokenSnapshot {
  total_tokens: number
  prompt_tokens: number
  completion_tokens: number
  cached_tokens?: number
  reasoning_tokens?: number
  cache_write_tokens?: number
  call_count?: number
  cache_hit_rate?: number
  cache_supported?: boolean
  model_context_limit: number
}

interface TaskContextBreakdown {
  context_limit: number
  system_prompt_tokens: number
  system_tools_tokens: number
  mcp_tools_tokens: number
  skills_tokens: number
  messages_tokens: number
}
```

## Goal & todos

```ts
type GoalStatus = 'active' | 'complete' | 'blocked'

interface Goal {
  objective: string
  status: GoalStatus
  tokens_used?: number
  created_at?: number
  updated_at?: number
}

interface TodoItem {
  id: number
  title: string
  status: 'pending' | 'in_progress' | 'completed' | 'cancelled'
}

interface QueuedMessage {
  id: string
  text: string
  images?: ChatImage[]
}
```

## Tool renderer props

```ts
// jcode-ui-core/adapters
interface ToolRendererProps {
  name: string
  args: string
  output?: string
  displayOutput?: string
  error?: string
  status: ToolStatus
  displayInfo?: ToolDisplayInfo
  children?: ToolCall[]
}

type ToolRenderer = ComponentType<ToolRendererProps>
```
