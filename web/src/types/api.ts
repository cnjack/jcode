// API types for jcode web backend

export interface HealthResponse {
  status: string
  version: string
  pwd: string
  provider: string
  model: string
  mode: string
}

export interface StatusResponse {
  running: boolean
  clients: number
  pwd: string
  provider: string
  model: string
  mode: string
}

export interface SessionItem {
  uuid: string
  created_at: string
  provider: string
  model: string
}

export interface SessionEntry {
  type: string
  content?: string
  name?: string
}

export interface ConfigResponse {
  provider: string
  model: string
  max_iterations: number
}

export interface TodoItem {
  ID: number
  Title: string
  Status: 'not-started' | 'in-progress' | 'completed'
}

export interface FileItem {
  name: string
  is_dir: boolean
  size: number
}

export interface FileContent {
  path: string
  content: string
}

// Model registry types
export interface ModelInfo {
  id: string
  name: string
  tool_call: boolean
  context_limit?: number
}

export interface ProviderInfo {
  id: string
  name: string
  models: ModelInfo[]
}

export interface ModelsResponse {
  current: { provider: string; model: string }
  providers: ProviderInfo[]
}

// Exec types
export interface ExecResponse {
  output: string
  exit_code: number
}

// Diff types
export interface DiffEntry {
  file: string
  patch: string
  additions: number
  deletions: number
  status: string // "M", "A", "D"
}

export interface DiffResponse {
  mode: string
  entries: DiffEntry[]
}

// MCP types
export interface MCPServerInfo {
  type: string
  command?: string
  url?: string
  status: string
  enabled: boolean
}

export interface MCPListResponse {
  servers: Record<string, MCPServerInfo>
}

// SSH types
export interface SSHAlias {
  name: string
  addr: string
  path?: string
}

export interface SSHListResponse {
  current: string
  aliases: SSHAlias[]
}

// Skill types (for slash commands)
export interface SkillInfo {
  name: string
  description: string
  slash?: string
}

// SSE event data types
export interface AgentTextData {
  text: string
}

export interface ToolCallData {
  name: string
  args: string
}

export interface ToolResultData {
  name: string
  output: string
  error?: string
}

export interface TokenUpdateData {
  prompt_tokens: number
  completion_tokens: number
  total_tokens: number
  model_context_limit: number
}

export interface AgentDoneData {
  error?: string
}

export interface ApprovalRequestData {
  id: string
  tool_name: string
  tool_args: string
  is_external: boolean
}

// UI message types
export type MessageRole = 'user' | 'assistant' | 'system'

export type AgentMode = 'agent' | 'plan'

export interface ChatMessage {
  id: string
  role: MessageRole
  content: string
  timestamp: number
}

export interface ToolCall {
  id: string
  name: string
  args: string
  output?: string
  error?: string
  status: 'running' | 'done' | 'error'
  timestamp: number
  /** For subagent tools: nested tool calls from within the subagent */
  children?: SubagentToolEvent[]
}

/** An intermediate tool call or tool result from a running subagent. */
export interface SubagentToolEvent {
  event: 'tool_call' | 'tool_result'
  toolName: string
  detail: string
  timestamp: number
}

/** Subagent lifecycle event (start/done). */
export interface SubagentEventData {
  name: string
  agent_type: string
  done: boolean
  result?: string
  error?: string
}

/** Subagent intermediate progress (inner tool calls). */
export interface SubagentProgressData {
  agent_name: string
  event: string // "tool_call" | "tool_result"
  tool_name: string
  detail: string
}

export interface PendingApproval {
  id: string
  tool_name: string
  tool_args: string
  is_external: boolean
  resolved?: boolean
  approved?: boolean
}

// Project management (localStorage)
export interface Project {
  id: string
  path: string
  createdAt: number
}

// Browse (folder picker)
export interface BrowseFolder {
  name: string
  path: string
}

export interface BrowseResponse {
  current: string
  folders: BrowseFolder[]
}
