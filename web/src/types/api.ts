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
}

export interface MCPListResponse {
  servers: Record<string, MCPServerInfo>
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

export type AgentMode = 'build' | 'plan'

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
  name: string
  path: string
  createdAt: number
}
