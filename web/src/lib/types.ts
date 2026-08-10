// API types for jcode web backend

export interface HealthResponse {
  status: string
  version: string
  pwd: string
  provider: string
  model: string
  agent?: string
  mode: string
}

export interface StatusResponse {
  running: boolean
  clients: number
  pwd: string
  provider: string
  model: string
  agent?: string
  mode: string
}

export interface CustomAgentInfo {
  name: string
  description: string
  model?: string
}

export interface SessionItem {
  uuid: string
  created_at: string
  provider: string
  model: string
  agent?: string
  title?: string
}

export interface SessionEntry {
  type: string
  uuid?: string
  project?: string
  provider?: string
  model?: string
  agent?: string
  content?: string
  name?: string
  args?: string
  output?: string
  error?: string
  tool_call_id?: string
  operation_id?: string
  outcome?: string
  error_code?: string
  artifact_ids?: string[]
  timestamp?: string

  // tool_call batch fields (concurrent calls from one assistant message)
  batch_id?: string
  batch_index?: number
  batch_size?: number
  started_at?: number
  // tool_result duration (ms; approval wait already subtracted)
  duration_ms?: number
  // tool_result: user rejected the call at the approval prompt (declined ≠ failed)
  denied?: boolean

  // Durable image-generation operation journal.
  operation_state?: 'dispatch_attempted' | 'accepted' | 'saving' | 'succeeded' | 'failed' | 'uncertain'
  operation_capability_key?: {
    provider_profile_id: string
    credential_kind?: string
    endpoint_profile: string
    model_id: string
  }

  // Metadata-only Artifact record. A missing storage kind is a legacy
  // workspace Artifact; managed paths are never sent to the browser.
  artifact_id?: string
  artifact_path?: string
  artifact_storage_kind?: 'workspace' | 'managed'
  artifact_key?: string
  artifact_title?: string
  artifact_kind?: string
  artifact_media_type?: string
  artifact_size?: number
  artifact_width?: number
  artifact_height?: number
  artifact_provider_id?: string
  artifact_model_id?: string
  artifact_revision?: number
  artifact_shareable?: boolean

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

  // mode_change / agent_change fields
  mode?: string

  // compact fields
  summary?: string
  compacted_n?: number

  // user message attachments (base64, no data: prefix) — mirrors session.EntryImage
  images?: { media_type: string; data: string }[]
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

export type ArtifactKind = 'text' | 'markdown' | 'code' | 'html' | 'image' | 'pdf' | 'csv' | 'binary'
export type ArtifactStatus = 'available' | 'missing' | 'unsupported' | 'too_large' | 'error'

export interface ArtifactRecord {
  id: string
  session_id: string
  storage_kind?: 'workspace' | 'managed'
  relative_path?: string
  relative_key?: string
  title: string
  kind: ArtifactKind
  media_type: string
  size: number
  width?: number
  height?: number
  sha256?: string
  provider_id?: string
  model_id?: string
  parent_artifact_id?: string
  operation_id?: string
  tool_call_id?: string
  revision: number
  updated_at: string
  status: ArtifactStatus
  focus?: boolean
  shareable?: boolean
}

export interface ArtifactShareResult {
  share_id: string
  url: string
  expires_at: string
}

export interface ArtifactShareSummary {
  share_id: string
  artifact_id: string
  revision: number
  state: string
  ciphertext_size: number
  ciphertext_sha256?: string
  expires_at: string
  completed_at?: string
  revoked_at?: string
  created_at: string
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
  // Always supplied by GET /api/models; only true entries belong in pickers.
  enabled: boolean
  image_support?: boolean
  input_modalities?: string[]
  output_modalities?: string[]
  capability_availability?: 'supported' | 'unsupported' | 'unknown'
  image_sizes?: string[]
  image_aspect_ratios?: string[]
  image_resolutions?: string[]
  // How this model exposes its reasoning/thinking controls (from models.dev).
  // Absent/empty ⇒ no reasoning controls to render.
  reasoning_options?: ReasoningOption[]
}

export interface ProviderInfo {
  id: string
  name: string
  /** Canonical provider family used for brand icon lookup and protocol hints. */
  kind: string
  /** Local providers call their endpoint directly; Cloud providers use cloud_proxy. */
  source: 'desktop' | 'cloud'
  scope?: 'cluster' | 'project'
  scope_id?: string
  scope_name?: string
  custom?: boolean // true for user-configured OpenAI-compatible providers
  models: ModelInfo[]
}

export interface ModelsResponse {
  current: { provider: string; model: string }
  current_image: { provider: string; model: string }
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

export interface GitBranchesResponse {
  current: string // empty if not a git repo
  branches: string[] // local branches, most-recently-committed first
}

export interface GitCheckoutResponse {
  branch: string // new current branch on success; '' when blocked
  blocked?: boolean // true when a plain switch was aborted by uncommitted changes
  message?: string // git's raw (C-locale) message when blocked
  files?: string[] // files that would be overwritten, parsed from git's output
  stashed?: boolean // true when changes were stashed as part of this switch
}

// A task = a conversation, listed across all projects for the sidebar tree.
export interface TaskItem {
  uuid: string
  project: string // project path
  created_at: string
  updated_at?: string
  provider: string
  model: string
  agent?: string
  title?: string
  pinned: boolean
  archived: boolean
  unread: boolean
  status?: string
  running?: boolean // a live engine for this task is currently running
  artifact_count?: number
  artifact_unseen?: boolean
}

export interface TaskMetaPatch {
  pinned?: boolean
  archived?: boolean
  unread?: boolean
  title?: string
}

// A project's persisted last-activity timestamp (bumped on session create /
// turn start / turn end). Deliberately NOT derived from surviving sessions:
// deleting a conversation must not move its project in the sidebar ordering.
export interface ProjectInfo {
  path: string
  updated_at?: string
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
  oauth_config?: {
    enabled: boolean
    client_id?: string
    /** Masked display value; never the stored secret. */
    client_secret?: string
    scopes?: string[]
    auth_server_metadata_url?: string
  }
  has_auth: boolean
  status: string // connected | needs_auth | error | disabled | configured
  error?: string
  scope?: string // global | project — which config layer defines this server
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
  remove_headers?: string[]
  timeout?: number
  oauth?: {
    enabled: boolean
    client_id?: string
    client_secret?: string
    scopes?: string[]
    remove_client_secret?: boolean
  }
  remove_oauth?: boolean
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

// Remote connection wizard
export type RemoteAuthMethod = 'password' | 'key'
export type RemoteKind = 'ssh' | 'docker'

export interface RemoteConnectRequest {
  type?: RemoteKind
  host?: string
  port?: number
  user?: string
  auth_method?: RemoteAuthMethod
  password?: string
  key_path?: string
  passphrase?: string
  container?: string // docker: container id or name
}

export interface RemoteConnectResponse {
  connection_id: string
  remote_pwd: string
  platform: string
  user?: string
  host?: string
  container?: string
}

export interface RemoteListDirResponse {
  path: string
  dirs: string[]
}

export interface RemoteBindResponse {
  status: string
  kind?: RemoteKind
  pwd: string
  label: string
  name: string
  host: string
  user: string
  port: number
  container?: string
  remote_path: string
}

export interface DockerContainer {
  id: string
  name: string
  image: string
  state: string
  status: string
  running: boolean
}

export interface DockerContainersResponse {
  containers: DockerContainer[]
}

// Skill types (for slash commands)
export interface SkillInfo {
  name: string
  description: string
  slash?: string
  builtin?: boolean
  source?: string // builtin | agents | user | project
  enabled?: boolean
}

// Slash command types (unified built-in + skill)
export interface SlashCommandInfo {
  slash: string
  description: string
  type: 'builtin' | 'skill' | 'flow'
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
  kind?: string
  collapsible?: boolean
}

export interface ToolResultStreams {
  stdout?: string
  stderr?: string
  aggregated?: string
}

export interface ToolResultMeta {
  exit_code?: number
  duration_ms?: number
  timed_out?: boolean
  truncated?: boolean
  spill_path?: string
}

export interface ToolResultPresentation {
  kind?: string
  title?: string
  subtitle?: string
  collapsible?: boolean
}

export interface ToolResultData {
  name: string
  output: string
  display_output?: string  // clean output for UI display (no metadata)
  error?: string
  tool_call_id?: string
  streams?: ToolResultStreams
  meta?: ToolResultMeta
  presentation?: ToolResultPresentation
}

export interface TokenUpdateData {
  // total_tokens is current context occupancy (last call); the cumulative
  // counters + cache_hit_rate cover the whole session.
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

export interface AgentDoneData {
  error?: string
}

// --- Usage statistics ---

export interface UsageDayBucket {
  date: string // YYYY-MM-DD
  tokens: number
  turns: number
  calls: number
}

export interface UsageShare {
  name: string
  tokens: number
  share: number // 0-1 fraction of grand total
}

export interface UsageTotals {
  total_tokens: number
  prompt_tokens: number
  completion_tokens: number
  cached_tokens: number
  reasoning_tokens: number
  calls: number
  turns: number
  sessions: number
}

export interface TaskContextBreakdown {
  context_limit: number
  system_prompt_tokens: number
  system_tools_tokens: number
  mcp_tools_tokens: number
  skills_tokens: number
  messages_tokens: number
}

export interface TaskStats {
  uuid: string
  is_active: boolean
  context?: TaskContextBreakdown
  cache_hit_rate: number
  cache_supported: boolean
  tokens: {
    total_tokens: number
    prompt_tokens: number
    completion_tokens: number
    cached_tokens: number
    reasoning_tokens: number
    calls: number
    turns?: number
  }
}

export interface UsageStats {
  range_days: number
  totals: UsageTotals
  active_days: number
  current_streak: number
  longest_streak: number
  most_used_model: string
  cache_hit_rate: number // 0-1
  cache_supported: boolean
  heatmap: UsageDayBucket[] // fixed ~365-day window
  daily_trend: UsageDayBucket[] // selected range
  by_model: UsageShare[]
  by_project: UsageShare[]
}

export interface ApprovalRequestData {
  id: string
  tool_name: string
  tool_args: string
  tool_call_id?: string // gated tool_call's id — marks that row as awaiting approval
  is_external: boolean
  task_id?: string // the task (engine) the approval belongs to; echoed back on resolve
  approval_class?: string
  options?: import('jcode-ui-core').ApprovalOption[]
  billable_summary?: import('jcode-ui-core').BillableApprovalSummary
  resolved_option_id?: string
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
  task_id?: string // the task (engine) the question belongs to; echoed back on resolve
}

export interface AskUserAnswer {
  question_header: string
  answer: string
  selected?: string[]
}

// UI message types
export type MessageRole = 'user' | 'assistant' | 'system'

export type AgentMode = 'approval' | 'plan' | 'auto' | 'full_access'

// normalizeMode maps a backend mode string to the UI's unified AgentMode.
export function normalizeMode(m?: string): AgentMode {
  if (m === 'plan') return 'plan'
  if (m === 'auto') return 'auto'
  if (m === 'full_access') return 'full_access'
  return 'approval'
}

export interface ChatImage {
  data: string       // base64 data (without data: prefix)
  media_type: string // e.g. "image/png", "image/jpeg"
  /** Original filename when known (file picker / paste). */
  name?: string
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
  // For assistant messages: how long the turn took (ms), from the user prompt
  // until the agent finished. Set on the final assistant message of a turn so
  // the elapsed time persists in the header after the live timer disappears.
  durationMs?: number
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
  streams?: ToolResultStreams
  meta?: ToolResultMeta
  presentation?: ToolResultPresentation
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
  // True while a resolve POST is in flight — disables the buttons so a slow/failed
  // request can't be double-submitted.
  resolving?: boolean
}

// Project management (localStorage)
export interface RemoteMeta {
  kind?: RemoteKind // defaults to 'ssh' for back-compat
  host: string // host:port as dialed (ssh)
  user: string
  port: number
  remotePath: string
  container?: string // docker container name/id
}

export interface Project {
  id: string
  path: string
  createdAt: number
  // Present for remote (SSH) workspaces. `path` is the host-qualified label
  // (ssh://user@host:port/remote/path); remote workspaces cannot be re-activated
  // by a local path switch and must be reconnected through the SSH wizard.
  remote?: RemoteMeta
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
  // Omitted by older servers. Clients must fall back to API-key auth so an
  // upgraded UI can still configure providers against an older sidecar.
  auth_methods?: ProviderCredentialMethod[]
}

export type AuthMethod = 'codex_oauth' | 'xai_oauth' | 'github_copilot'
export type ProviderCredentialMethod = 'api_key' | AuthMethod

export interface ProviderAuthBinding {
  method: AuthMethod
  // Omission follows the managed-auth default account for this method.
  account_id?: string
}

export interface ProviderAuthAccount {
  id: string
  login: string
  email?: string
  domain?: string
  authenticated_at: string
  requires_reauth: boolean
}

export interface ProviderAuthStatus {
  method: AuthMethod
  accounts: ProviderAuthAccount[]
  default_account_id?: string
}

export interface ProviderAuthFlow {
  flow_id: string
  user_code: string
  verification_uri: string
  verification_uri_complete?: string
  expires_at: string
  interval_seconds?: number
  // Compatibility with the first POC contract; new servers use
  // interval_seconds.
  interval?: number
}

export interface ProviderAuthPollResult {
  state: 'pending' | 'authorized' | 'denied' | 'expired' | 'error'
  // Device providers may raise the interval after a slow_down response. Always
  // schedule the next poll from the latest response instead of the start flow.
  interval_seconds?: number
  interval?: number
  account?: ProviderAuthAccount
  error?: string
}

// ReasoningOption mirrors models.dev's reasoning_options: how a model exposes
// its thinking controls. type is 'effort' | 'toggle' | 'budget_tokens'.
export interface ReasoningOption {
  type: string
  values?: string[]
  min?: number
  max?: number
}

export interface SetupModel {
  id: string
  name: string
  tool_call: boolean
  context_limit?: number
  reasoning?: boolean
  attachment?: boolean
  reasoning_options?: ReasoningOption[]
}

// A model attached to a provider via config (custom providers always have at
// least one; registry providers may add extras not yet in the registry).
export interface CustomModelDetail {
  id: string
  name?: string
  reasoning?: boolean
  context?: number // context window in tokens
  attachment?: boolean // accepts image inputs (vision)
  effort_tiers?: string[] // selectable reasoning-effort levels, e.g. ['low','medium','high']
  custom?: boolean // true = user-defined (editable); false/undefined = built-in registry (read-only)
}

export interface ProviderDetail {
  id: string
  name?: string // display name for custom (non-registry) providers
  custom?: boolean // true if this provider is not in the registry
  api_key_set: boolean
  auth_binding?: ProviderAuthBinding
  auth_status?: ProviderAuthStatus
  auth_methods?: ProviderCredentialMethod[]
  api_key?: string
  base_url?: string
  headers?: Record<string, string> // values masked
  custom_models?: CustomModelDetail[]
  vision?: boolean // provider-level image-input override (null ⇒ registry default)
  thinking?: boolean // provider-level extended-reasoning toggle
  reasoning_effort?: string // provider-level default effort
  provider_tools?: Record<string, ProviderToolPolicy>
  image_endpoint?: ImageEndpointConfig
  capabilities?: ProviderCapability[]
}

export interface ProviderCapability {
  id: 'image_generation' | 'web_search' | string
  availability: 'supported' | 'unsupported' | 'unknown'
  mechanism?: string
  model_label?: string
  enabled: boolean
  max_calls_per_turn: number
  max_calls_per_session: number
}

export interface ProviderToolPolicy {
  enabled?: boolean
  max_calls_per_turn?: number
  max_calls_per_session?: number
}

export interface ImageEndpointConfig {
  protocol?: string
  base_url?: string
  models?: { id: string; name?: string; sizes?: string[] }[]
  asset_hosts?: string[]
}

// Advanced provider settings shared by the add/update payloads. Capabilities
// (vision/thinking/reasoning_effort) are model-level, so they're not part of
// provider configuration.
export interface ProviderAdvanced {
  base_url?: string
  headers?: Record<string, string>
}

// Result of a connection test against a provider's /models endpoint. On success
// it carries the measured latency and the number of advertised models; on
// failure the error is classified (auth | network | server) so the UI can show
// a targeted message.
export interface ValidateResult {
  valid: boolean
  latency_ms?: number
  model_count?: number
  error?: string
  error_type?: 'auth' | 'network' | 'server' | ''
}

// A model entry in a provider's browsable catalog (the "browse directory" UI).
// `added` reflects whether the model is already configured for this provider,
// so each row renders as "+ add" or "✓ added" (toggle to remove).
export interface CatalogModel {
  id: string
  name?: string
  added?: boolean
  context?: number
  reasoning?: boolean
  attachment?: boolean
  effort_tiers?: string[] // selectable reasoning-effort levels (custom models)
  custom?: boolean // true = user-defined (editable/removable); false/undefined = registry model
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
  // Per-"provider/model" reasoning-effort choices made from the chat picker.
  effort_overrides?: Record<string, string>
}

export interface ApprovalReviewConfig {
  // '' = follow small_model, then the main model. 'small' = the small_model
  // alias. Otherwise a concrete "provider/model" ref.
  model?: string
  policy?: string
  // 0 = use defaults.timeout_seconds.
  timeout_seconds?: number
  investigate?: boolean
  reuse_session?: boolean
  // '' = use defaults.audit_path.
  audit_path?: string
}

// ApprovalReviewDefaults carries the server's resolved built-in defaults, so
// the settings form can show what an empty field actually does without
// hardcoding the values. Never sent back on save.
export interface ApprovalReviewDefaults {
  timeout_seconds: number
  audit_path: string
}

export interface ApprovalReviewConfigResponse extends ApprovalReviewConfig {
  defaults?: ApprovalReviewDefaults
}
