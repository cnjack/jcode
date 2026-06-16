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
  title?: string
}

export interface SessionEntry {
  type: string
  uuid?: string
  project?: string
  provider?: string
  model?: string
  content?: string
  name?: string
  args?: string
  output?: string
  error?: string
  tool_call_id?: string
  timestamp?: string

  // plan_update fields
  plan_status?: string
  plan_title?: string
  plan_content?: string
  feedback?: string

  // todo_snapshot fields
  todos?: { id: number; title: string; status: string }[]

  // subagent fields
  subagent_name?: string
  subagent_type?: string

  // mode_change field
  mode?: string

  // compact fields
  summary?: string
  compacted_n?: number
}

export interface ConfigResponse {
  provider: string
  model: string
  max_iterations: number
}

export interface TodoItem {
  id: number
  title: string
  status: 'pending' | 'in_progress' | 'completed' | 'cancelled'
}

export interface FileItem {
  name: string
  is_dir: boolean
  size: number
}

export type GoalStatus = 'active' | 'complete' | 'blocked'

export interface Goal {
  objective: string
  status: GoalStatus
  tokens_used: number
  created_at: number
  updated_at: number
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
  reasoning?: boolean
  recommended?: boolean
  default_enabled?: boolean
  enabled?: boolean
  image_support?: boolean
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

export interface WorkspaceInfo {
  branch: string // empty if not a git repo
  dirty: boolean
}

// A task = a conversation, listed across all projects for the sidebar tree.
export interface TaskItem {
  uuid: string
  project: string // project path
  created_at: string
  provider: string
  model: string
  title?: string
  pinned: boolean
  archived: boolean
  unread: boolean
  status?: string
}

export interface TaskMetaPatch {
  pinned?: boolean
  archived?: boolean
  unread?: boolean
  title?: string
}

// MCP types
export interface MCPServerInfo {
  name: string
  type: string
  command?: string
  url?: string
  args?: string[]
  env?: string[]
  headers?: Record<string, string>
  timeout?: number
  enabled: boolean
  oauth: boolean
  has_auth: boolean
  status: string // connected | needs_auth | error | disabled | configured
  error?: string
}

export interface MCPListResponse {
  servers: Record<string, MCPServerInfo>
}

// Request body for creating/updating an MCP server.
export interface MCPServerRequest {
  name: string
  type: string // local | http | sse
  url?: string
  command?: string
  args?: string[]
  env?: string[]
  headers?: Record<string, string>
  timeout?: number
  oauth?: {
    enabled: boolean
    client_id?: string
    client_secret?: string
    scopes?: string[]
  }
}

export interface MCPLoginStatus {
  status: string // idle | pending | authorized | error | needs_client_id
  auth_url?: string
  message?: string
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
  builtin?: boolean
  source?: string // builtin | local
  enabled?: boolean
}

// Slash command types (unified built-in + skill)
export interface SlashCommandInfo {
  slash: string
  description: string
  type: 'builtin' | 'skill'
}

// WebSocket event data types
export interface AgentTextData {
  text: string
}

export interface ToolCallData {
  name: string
  args: string
  tool_call_id?: string
  display_info?: ToolDisplayInfo
}

export interface ToolDisplayInfo {
  title: string
  subtitle?: string
  icon?: string
  category?: string // 'context' | 'mutation' | 'execution'
}

export interface ToolResultData {
  name: string
  output: string
  display_output?: string  // clean output for UI display (no metadata)
  error?: string
  tool_call_id?: string
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

// ask_user types — an interactive question (or batch of questions) the agent
// poses mid-run. Mirrors the Go AskUserQuestion / AskUserAnswer structs.
export interface AskUserOption {
  label: string
  description?: string
}

export interface AskUserQuestion {
  question: string
  header?: string
  options?: AskUserOption[]
  multi_select?: boolean
}

export interface AskUserRequestData {
  id: string
  questions: AskUserQuestion[]
}

export interface AskUserAnswer {
  question_header: string
  answer: string
  selected?: string[]
}

// UI message types
export type MessageRole = 'user' | 'assistant' | 'system'

export type AgentMode = 'ask' | 'plan' | 'autopilot'

// normalizeMode maps any backend/legacy mode string to a unified AgentMode,
// tolerating the old 'build'/'agent' aliases so older servers still work.
export function normalizeMode(m?: string): AgentMode {
  if (m === 'plan') return 'plan'
  if (m === 'autopilot' || m === 'auto') return 'autopilot'
  return 'ask' // ask / agent / build / normal / executing / empty
}

export interface ChatImage {
  data: string       // base64 data (without data: prefix)
  media_type: string // e.g. "image/png", "image/jpeg"
}

export interface ChatMessage {
  id: string
  role: MessageRole
  content: string
  timestamp: number
  source?: string
  images?: ChatImage[]
  // For system messages: 'error' renders as a real error, 'notice' as a calm
  // notice (e.g. "Stopped"). Undefined keeps the default system styling.
  level?: 'error' | 'notice'
  // Optional raw detail (e.g. the unmapped error string) shown collapsed.
  detail?: string
}

export interface ToolCall {
  id: string
  toolCallID?: string  // backend tool_call_id for precise result matching
  name: string
  args: string
  output?: string
  displayOutput?: string  // clean output for UI display
  error?: string
  status: 'running' | 'done' | 'error'
  timestamp: number
  /** Display metadata extracted from tool args */
  displayInfo?: ToolDisplayInfo
  /** For subagent tools: nested tool calls from within the subagent */
  children?: ToolCall[]
  /**
   * For ask_user tools: the request id from the ask_user_request event. Present
   * only while the question is awaiting an answer (live runs, not replay).
   * Cleared once answered/resolved so the interactive UI collapses.
   */
  askUserId?: string
  /** For ask_user tools: the backend-normalized questions to render. */
  askUserQuestions?: AskUserQuestion[]
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

// Setup types
export interface SetupProvider {
  id: string
  name: string
  doc?: string
  api?: string
  env?: string[]
  configured: boolean
  tag?: string // "recommended", "local", etc.
}

export interface SetupModel {
  id: string
  name: string
  tool_call: boolean
  context_limit?: number
  reasoning?: boolean
}

export interface ProviderDetail {
  id: string
  api_key_set: boolean
  api_key?: string
  base_url?: string
}

// Model state types
export interface ModelRef {
  provider: string
  model: string
}

export interface ModelStateResponse {
  recent: ModelRef[]
  favorite: ModelRef[]
  enabled_models: ModelRef[]
  disabled_models: ModelRef[]
}
