// API client for jcode backend — ported from web/src/composables/api.ts.
import type { ModelsResponse, AgentMode, ExecResponse, DiffResponse, WorkspaceInfo, GitBranchesResponse, GitCheckoutResponse, TaskItem, TaskMetaPatch, MCPListResponse, MCPServerRequest, MCPLoginStatus, BrowseResponse, SSHListResponse, SkillInfo, SlashCommandInfo, TodoItem, Goal, SessionItem, SessionEntry, FileItem, SetupProvider, SetupModel, ProviderDetail, ProviderAdvanced, CustomModelDetail, ValidateResult, CatalogModel, ModelStateResponse, ChatImage, AskUserAnswer, AskUserRequestData, ApprovalRequestData, RemoteConnectRequest, RemoteConnectResponse, RemoteListDirResponse, RemoteBindResponse, DockerContainersResponse, UsageStats, TaskStats, TokenUpdateData, ApprovalReviewConfig, ApprovalReviewConfigResponse } from './types'
import type { AutomationItem, AutomationRun, AutomationTemplate, AutomationCreate, Automation } from './automation'
import { apiBase } from './apiBase'
import { getAuthToken, notifyAuthExpired } from './authToken'

interface RequestOptions extends RequestInit {
  /**
   * Skip auto Authorization injection AND the global 401 handler. Used by
   * authVerify, where a 401 means "wrong token typed in the login page" and must
   * surface as an error in that form — not clear the token / re-trigger the gate.
   */
  skipAuth?: boolean
}

async function request<T>(path: string, options?: RequestOptions): Promise<T> {
  const token = getAuthToken()
  // Normalize to a Headers instance so every HeadersInit form (plain object,
  // Headers, tuple array) is preserved rather than silently dropped.
  const headers = new Headers(options?.headers)
  if (!headers.has('Content-Type')) headers.set('Content-Type', 'application/json')
  if (token && !options?.skipAuth && !headers.has('Authorization')) {
    headers.set('Authorization', `Bearer ${token}`)
  }
  const resp = await fetch(`${apiBase}${path}`, { ...options, headers })
  if (resp.status === 401 && !options?.skipAuth) {
    notifyAuthExpired()
  }
  if (!resp.ok) {
    const body = await resp.json().catch(() => ({ error: resp.statusText }))
    // Attach the status so callers can distinguish 401 (bad token) from
    // transport/5xx failures and react differently.
    const err = new Error(body.error || `HTTP ${resp.status}`) as Error & { status?: number }
    err.status = resp.status
    throw err
  }
  return resp.json()
}

export const api = {
  truncateHistory: (beforeUserMessage: number) =>
    request<{ status: string; session_id: string }>('/api/history/truncate', {
      method: 'POST',
      body: JSON.stringify({ before_user_message: beforeUserMessage }),
    }),
  health: () =>
    request<{ status: string; version: string; pwd: string; provider: string; model: string; mode: string; session_id: string; running: boolean; image_support?: boolean; needs_setup?: boolean; auth_required?: boolean }>(
      '/api/health',
    ),
  // authVerify validates a token typed into the login gate. skipAuth keeps a 401
  // (wrong token) from tripping the global expiry handler — the gate shows it.
  authVerify: (token: string) =>
    request<{ ok: boolean }>('/api/auth/verify', {
      method: 'POST',
      skipAuth: true,
      headers: { Authorization: `Bearer ${token}` },
    }),
  status: () =>
    request<{
      running: boolean
      ws_clients: number
      pwd: string
      provider: string
      model: string
      mode: string
      token?: TokenUpdateData
    }>('/api/status'),
  config: () => request<{ provider: string; model: string; small_model: string; max_iterations: number }>('/api/config'),
  usageStats: (days = 30) => request<UsageStats>(`/api/usage/stats?days=${days}`),
  taskStats: (id: string) => request<TaskStats>(`/api/tasks/${encodeURIComponent(id)}/stats`),
  todos: () => request<TodoItem[]>('/api/todos'),
  goal: () => request<Goal | null>('/api/goal'),
  setGoal: (objective: string, start = true) =>
    request<Goal>('/api/goal', {
      method: 'POST',
      body: JSON.stringify({ objective, start }),
    }),
  clearGoal: () => request<{ status: string }>('/api/goal', { method: 'DELETE' }),
  sessions: () => request<SessionItem[]>('/api/sessions'),
  session: (id: string) => request<SessionEntry[]>(`/api/sessions/${encodeURIComponent(id)}`),
  deleteSession: (id: string) =>
    request<{ status: string }>(`/api/sessions/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  newSession: (sessionId?: string) =>
    request<{ status: string; session_id: string }>('/api/sessions', {
      method: 'POST',
      body: sessionId ? JSON.stringify({ session_id: sessionId }) : undefined,
    }),
  files: (path?: string) => {
    const q = path ? `?path=${encodeURIComponent(path)}` : ''
    return request<FileItem[]>(`/api/files${q}`)
  },
  fileContent: (path: string) =>
    request<{ path: string; content: string }>(`/api/files/content?path=${encodeURIComponent(path)}`),
  chat: (message: string, mode?: AgentMode, sessionId?: string, images?: ChatImage[]) =>
    request<{ status: string; session_id: string }>('/api/chat', {
      method: 'POST',
      body: JSON.stringify({
        message,
        mode,
        session_id: sessionId || undefined,
        images: images && images.length > 0 ? images : undefined,
      }),
    }),
  approval: (id: string, approved: boolean, approveAll = false, taskId?: string) =>
    request<{ status: string }>('/api/approval', {
      method: 'POST',
      body: JSON.stringify({ id, approved, approve_all: approveAll, task_id: taskId }),
    }),
  askUser: (id: string, answers: AskUserAnswer[], taskId?: string) =>
    request<{ status: string }>('/api/ask', {
      method: 'POST',
      body: JSON.stringify({ id, answers, task_id: taskId }),
    }),
  askPending: () => request<AskUserRequestData[]>('/api/ask/pending'),
  approvalPending: () => request<ApprovalRequestData[]>('/api/approval/pending'),
  models: () => request<ModelsResponse>('/api/models'),
  switchModel: (provider: string, model: string) =>
    request<{ status: string }>('/api/model', {
      method: 'POST',
      body: JSON.stringify({ provider, model }),
    }),
  // Set or clear config.small_model (both empty = clear). Persisted server-side;
  // takes effect immediately (subagent "small" alias, session titles).
  setSmallModel: (provider: string, model: string) =>
    request<{ status: string; small_model: string }>('/api/small-model', {
      method: 'POST',
      body: JSON.stringify({ provider, model }),
    }),
  switchMode: (mode: string) =>
    request<{ status: string; mode: string }>('/api/mode', {
      method: 'POST',
      body: JSON.stringify({ mode }),
    }),
  exec: (command: string) =>
    request<ExecResponse>('/api/exec', {
      method: 'POST',
      body: JSON.stringify({ command }),
    }),
  diff: (mode?: string) => {
    const q = mode ? `?mode=${encodeURIComponent(mode)}` : ''
    return request<DiffResponse>(`/api/diff${q}`)
  },
  workspace: () => request<WorkspaceInfo>('/api/workspace'),
  gitBranches: () => request<GitBranchesResponse>('/api/git/branches'),
  gitCheckout: (branch: string, create = false, strategy: '' | 'stash' | 'force' = '') =>
    request<GitCheckoutResponse>('/api/git/checkout', {
      method: 'POST',
      body: JSON.stringify({ branch, create, strategy }),
    }),
  tasks: () => request<TaskItem[]>('/api/tasks'),
  updateTask: (id: string, patch: TaskMetaPatch) =>
    request<TaskItem>(`/api/tasks/${encodeURIComponent(id)}`, {
      method: 'PATCH',
      body: JSON.stringify(patch),
    }),
  mcpList: () => request<MCPListResponse>('/api/mcp'),
  mcpToggle: (name: string, enabled: boolean) =>
    request<{ status: string }>(`/api/mcp/${encodeURIComponent(name)}/toggle`, {
      method: 'POST',
      body: JSON.stringify({ enabled }),
    }),
  mcpCreate: (data: MCPServerRequest) =>
    request<{ status: string; name: string }>('/api/mcp/servers', {
      method: 'POST',
      body: JSON.stringify(data),
    }),
  mcpUpdate: (name: string, data: MCPServerRequest) =>
    request<{ status: string; name: string }>(`/api/mcp/servers/${encodeURIComponent(name)}`, {
      method: 'PUT',
      body: JSON.stringify(data),
    }),
  mcpDelete: (name: string) =>
    request<{ status: string }>(`/api/mcp/servers/${encodeURIComponent(name)}`, {
      method: 'DELETE',
    }),
  mcpLogin: (name: string) =>
    request<MCPLoginStatus>(`/api/mcp/${encodeURIComponent(name)}/login`, { method: 'POST' }),
  mcpLoginStatus: (name: string) =>
    request<MCPLoginStatus>(`/api/mcp/${encodeURIComponent(name)}/login/status`),
  browse: (path?: string) => {
    const q = path ? `?path=${encodeURIComponent(path)}` : ''
    return request<BrowseResponse>(`/api/browse${q}`)
  },
  switchProject: (path: string) =>
    request<{ status: string; pwd: string }>('/api/project/switch', {
      method: 'POST',
      body: JSON.stringify({ path }),
    }),
  // Returns the subset of the given local paths that no longer exist on disk, so
  // the workspace picker can hide dead entries. Send local paths only — ssh://
  // labels can't be stat'd server-side.
  validatePaths: (paths: string[]) =>
    request<{ missing: string[] }>('/api/project/validate', {
      method: 'POST',
      body: JSON.stringify({ paths }),
    }),
  ptyCreate: () =>
    request<{ id: string }>('/api/pty', { method: 'POST' }),
  ptyList: () =>
    request<{ sessions: string[] }>('/api/pty'),
  ptyKill: (id: string) =>
    request<{ status: string }>(`/api/pty/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  approvalMode: () =>
    request<{ auto_approve: boolean }>('/api/approval/mode'),
  setApprovalMode: (autoApprove: boolean) =>
    request<{ auto_approve: boolean }>('/api/approval/mode', {
      method: 'POST',
      body: JSON.stringify({ auto_approve: autoApprove }),
    }),
  stop: (taskId?: string) =>
    request<{ status: string }>('/api/stop', {
      method: 'POST',
      body: JSON.stringify({ task_id: taskId || '' }),
    }),
  sshList: () =>
    request<SSHListResponse>('/api/ssh'),
  dockerContainers: () =>
    request<DockerContainersResponse>('/api/docker/containers'),

  // Remote connection wizard (SSH)
  remoteConnect: (data: RemoteConnectRequest) =>
    request<RemoteConnectResponse>('/api/remote/connect', {
      method: 'POST',
      body: JSON.stringify(data),
    }),
  remoteListDir: (connectionId: string, path: string) =>
    request<RemoteListDirResponse>('/api/remote/list-dir', {
      method: 'POST',
      body: JSON.stringify({ connection_id: connectionId, path }),
    }),
  remoteBind: (connectionId: string, path: string) =>
    request<RemoteBindResponse>('/api/remote/bind', {
      method: 'POST',
      body: JSON.stringify({ connection_id: connectionId, path }),
    }),
  remoteCancel: (connectionId: string) =>
    request<{ status: string }>('/api/remote/cancel', {
      method: 'POST',
      body: JSON.stringify({ connection_id: connectionId }),
    }),
  remoteSaveAlias: (name: string, addr: string, path: string) =>
    request<{ status: string }>('/api/remote/save-alias', {
      method: 'POST',
      body: JSON.stringify({ name, addr, path }),
    }),
  remoteSaveDockerAlias: (name: string, container: string, path: string) =>
    request<{ status: string }>('/api/remote/save-docker-alias', {
      method: 'POST',
      body: JSON.stringify({ name, container, path }),
    }),
  skillsList: () =>
    request<SkillInfo[]>('/api/skills'),
  skillToggle: (name: string, enabled: boolean) =>
    request<{ status: string }>(`/api/skills/${encodeURIComponent(name)}/toggle`, {
      method: 'POST',
      body: JSON.stringify({ enabled }),
    }),
  slashCommands: () =>
    request<SlashCommandInfo[]>('/api/slash-commands'),
  channelStatus: () =>
    request<{ available: boolean; channel?: string; state?: string }>('/api/channel'),
  channelLogin: () =>
    request<{ status: string; qr_content: string }>('/api/channel/login', { method: 'POST' }),
  channelLogout: () =>
    request<{ status: string; state: string }>('/api/channel/logout', { method: 'POST' }),
  channelEnable: () =>
    request<{ status: string; state: string }>('/api/channel/enable', { method: 'POST' }),
  channelDisable: () =>
    request<{ status: string; state: string }>('/api/channel/disable', { method: 'POST' }),
  channelBLEStatus: () =>
    request<{ enabled: boolean; available: boolean }>('/api/channel/ble'),
  setChannelBLE: (enabled: boolean) =>
    request<{ enabled: boolean }>('/api/channel/ble', {
      method: 'POST',
      body: JSON.stringify({ enabled }),
    }),

  // Setup API
  setupProviders: () =>
    request<SetupProvider[]>('/api/setup/providers'),
  setupProviderModels: (providerId: string) =>
    request<SetupModel[]>(`/api/setup/providers/${encodeURIComponent(providerId)}/models`),
  setupComplete: (data: { provider: string; api_key: string; model?: string; model_reasoning?: boolean; base_url?: string; name?: string; headers?: Record<string, string> }) =>
    request<{ status: string; provider: string; model: string }>('/api/setup/complete', {
      method: 'POST',
      body: JSON.stringify(data),
    }),
  setupStatus: () =>
    request<{ needs_setup: boolean }>('/api/setup/status'),
  setupValidate: (data: { provider: string; api_key: string; base_url?: string; headers?: Record<string, string> }) =>
    request<ValidateResult>('/api/setup/validate', {
      method: 'POST',
      body: JSON.stringify(data),
    }),

  // Provider management
  listProviders: () =>
    request<ProviderDetail[]>('/api/providers'),
  // vision is deliberately absent: image support is model metadata, and the
  // backend treats an omitted field as "clear the stored override".
  addProvider: (data: { id: string; api_key: string; name?: string; model?: string; model_reasoning?: boolean; thinking?: boolean; reasoning_effort?: string } & ProviderAdvanced) =>
    request<{ status: string }>('/api/providers', {
      method: 'POST',
      body: JSON.stringify(data),
    }),
  updateProvider: (id: string, data: { api_key?: string; name?: string; custom_models?: CustomModelDetail[]; thinking?: boolean; reasoning_effort?: string } & ProviderAdvanced) =>
    request<{ status: string }>(`/api/providers/${encodeURIComponent(id)}`, {
      method: 'PUT',
      body: JSON.stringify(data),
    }),
  deleteProvider: (id: string) =>
    request<{ status: string }>(`/api/providers/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  // Browse a provider's model catalog (built-in for registry providers, live
  // /models for custom endpoints). Each entry is flagged added=true when already
  // configured, so the UI renders "+ add" / "✓ added" toggles.
  providerCatalog: (id: string) =>
    request<CatalogModel[]>(`/api/providers/${encodeURIComponent(id)}/models`),

  // Model state
  modelState: () =>
    request<ModelStateResponse>('/api/model-state'),
  toggleFavorite: (provider: string, model: string) =>
    request<{ favorite: boolean }>('/api/model-state/favorite', {
      method: 'POST',
      body: JSON.stringify({ provider, model }),
    }),
  toggleModelEnabled: (provider: string, model: string, enabled: boolean) =>
    request<{ enabled: boolean }>('/api/model-state/enabled', {
      method: 'POST',
      body: JSON.stringify({ provider, model, enabled }),
    }),
  setModelEffort: (provider: string, model: string, effort: string) =>
    request<{ effort: string }>('/api/model-state/effort', {
      method: 'POST',
      body: JSON.stringify({ provider, model, effort }),
    }),

  // Automations
  automations: () => request<AutomationItem[]>('/api/automations'),
  automationCreate: (data: AutomationCreate) =>
    request<AutomationItem>('/api/automations', { method: 'POST', body: JSON.stringify(data) }),
  automationUpdate: (id: string, data: Partial<Automation>) =>
    request<AutomationItem>(`/api/automations/${encodeURIComponent(id)}`, {
      method: 'PUT',
      body: JSON.stringify(data),
    }),
  automationDelete: (id: string) =>
    request<{ status: string }>(`/api/automations/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  automationRunNow: (id: string) =>
    request<{ status: string }>(`/api/automations/${encodeURIComponent(id)}/run`, { method: 'POST' }),
  automationRuns: (automationId?: string) => {
    const q = automationId ? `?automation_id=${encodeURIComponent(automationId)}` : ''
    return request<AutomationRun[]>(`/api/automations/runs${q}`)
  },
  automationTemplates: () => request<AutomationTemplate[]>('/api/automation-templates'),

  // Browser use
  browserStatus: () => request<BrowserStatusResponse>('/api/browser/status'),
  browserSaveConfig: (data: BrowserConfig) =>
    request<{ status: string }>('/api/browser/config', { method: 'POST', body: JSON.stringify(data) }),

  // Computer use
  computerStatus: () => request<ComputerStatusResponse>('/api/computer/status'),
  computerSaveConfig: (data: ComputerConfig) =>
    request<ComputerConfigSaveResponse>('/api/computer/config', { method: 'POST', body: JSON.stringify(data) }),
  computerRequestPermissions: (data: ComputerPermissionRequest) =>
    request<ComputerPermissionRequestResponse>('/api/computer/permissions', { method: 'POST', body: JSON.stringify(data) }),

  // Approval review tuning (Auto session mode)
  approvalReviewConfig: () => request<ApprovalReviewConfigResponse>('/api/approval-review-config'),
  setApprovalReviewConfig: (data: ApprovalReviewConfig) =>
    request<{ status: string }>('/api/approval-review-config', { method: 'POST', body: JSON.stringify(data) }),

  // Progressive tool disclosure
  toolSearchStatus: () => request<ToolSearchStatusResponse>('/api/tool-search/status'),
  toolSearchConfig: (data: ToolSearchConfig) =>
    request<ToolSearchConfigResponse>('/api/tool-search/config', { method: 'POST', body: JSON.stringify(data) }),

  // Developer options (logging + tracing)
  devOptionsStatus: () => request<DevOptionsStatusResponse>('/api/dev-options/status'),
  devOptionsConfig: (data: DevOptionsConfig) =>
    request<DevOptionsConfigResponse>('/api/dev-options/config', { method: 'POST', body: JSON.stringify(data) }),

  // Project memory and background consolidation (Dream)
  memoryStatus: () => request<MemoryStatusResponse>('/api/memory/status'),
  memoryConfig: (data: MemoryConfig) =>
    request<MemoryConfigResponse>('/api/memory/config', { method: 'POST', body: JSON.stringify(data) }),
  memorySync: (project: string) =>
    request<MemoryActionResponse>('/api/memory/sync', { method: 'POST', body: JSON.stringify({ project }) }),
  memoryClear: (project: string) =>
    request<MemoryActionResponse>(`/api/memory?scope=project&project=${encodeURIComponent(project)}`, { method: 'DELETE' }),
}

export interface ToolSearchConfig {
  enabled: boolean
}

export interface ToolSearchStatusResponse {
  available?: boolean
  supported?: boolean
  enabled: boolean
  direct_count?: number
  deferred_count?: number
  mcp_deferred_count?: number
  refresh_warning?: string
  warning?: string
}

export interface ToolSearchConfigResponse extends ToolSearchStatusResponse {
  status?: string
  warning_code?: string
}

export interface DevOptionsConfig {
  logging_enabled?: boolean
  tracing_enabled?: boolean
  langfuse?: {
    host?: string
    public_key?: string
    secret_key?: string
  }
  langfuse_clear?: boolean
}

export interface DevOptionsLangfuseStatus {
  host: string
  public_key: string
  public_key_set: boolean
  secret_key_set: boolean
  default_host: string
}

export interface DevOptionsStatusResponse {
  available?: boolean
  logging_enabled: boolean
  tracing_enabled: boolean
  langfuse_configured?: boolean
  langfuse?: DevOptionsLangfuseStatus
}

export interface DevOptionsConfigResponse {
  status?: string
  logging_enabled: boolean
  tracing_enabled: boolean
  restart_required?: boolean
}

export interface MemoryConfig {
  enabled: boolean
  generate: boolean
  /** Empty follows the main chat model. */
  model?: string
  daily_token_budget: number
  cooldown_hours: number
  max_age_days: number
  max_unused_days: number
  phase2_top_n: number
  summary_inject_tokens: number
}

export interface MemoryStatusResponse {
  available?: boolean
  supported?: boolean
  remote?: boolean
  reason?: string
  error?: string
  config?: Partial<MemoryConfig>
  enabled?: boolean
  generate?: boolean
  /** Effective pipeline state after applying the master memory switch. */
  effective_generate?: boolean
  model?: string
  project?: string
  project_root?: string
  memory_path?: string
  running?: boolean
  busy?: boolean
  today_tokens?: number
  daily_token_budget?: number
  notes_count?: number
  inbox_count?: number
  tracked_files?: number
  extracted_count?: number
  failed_count?: number
  summary_size?: number
  summary_bytes?: number
  summary_exists?: boolean
  summary_modified_at?: string
  last_pipeline_at?: string
  last_consolidation_at?: string
  last_consolidation?: string
  last_consolidation_noop?: boolean
  last_consolidation_decisions?: Record<string, number>
  warning?: string
}

export interface MemoryConfigResponse {
  status?: string
  config?: Partial<MemoryConfig>
  warning?: string
  warning_code?: string
}

export interface MemoryActionResponse {
  status?: string
  scope?: string
  running?: boolean
  busy?: boolean
  message?: string
  warning?: string
  warning_code?: string
}

export interface BrowserSitePermission {
  origin: string
  navigate?: string
  interact?: string
}

export interface BrowserConfig {
  enabled: boolean
  backend: string
  chrome_path?: string
  headless?: boolean
  viewport?: string
  approval?: Record<string, string>
  site_permissions?: BrowserSitePermission[]
  dev_mode?: boolean
}

export interface BrowserStatusResponse {
  available: boolean
  status?: {
    enabled: boolean
    backend: string
    chrome_found: boolean
    chrome_path?: string
    chrome_version?: string
    extension_online: boolean
    dev_mode: boolean
  }
  site_permissions?: BrowserSitePermission[]
  approval?: Record<string, string>
}

// ─── computer use ───────────────────────────────────────────────────────────
// Hand-mirrored from Go. Sources, in order of authority:
//   config.ComputerAppPermission / config.ComputerConfig (internal/config/config.go)
//   computer.Status                                      (internal/computer/manager.go)
//   web.handleComputerStatus                             (internal/web/computer.go)
// See internal-doc/computer-use-design.md §5.

/** Per-app override. `tier` may only *tighten* the built-in tier for that app;
 *  the backend (computer.Manager.TierOverrides) drops a row that tries to
 *  loosen one, so the UI must never offer a tier above the built-in default. */
export interface ComputerAppPermission {
  bundle_id: string
  tier?: string // read | click | full; '' = built-in default
  launch?: string // ask | allow
  interact?: string // ask | allow
}

export interface ComputerConfig {
  enabled: boolean
  /** Per-class defaults: 'launch' and 'interact' → 'ask' | 'always_allow'.
   *  Clipboard reads are deliberately absent — they always prompt (design §4.4). */
  approval?: Record<string, string>
  app_permissions?: ComputerAppPermission[]
  max_actions_per_batch?: number
  clipboard_read?: boolean
  clipboard_write?: boolean
  system_key_combos?: boolean
}

export interface ComputerConfigSaveResponse {
  status: string
  config: ComputerConfig
  warning_code?: 'agent_refresh_failed'
}

/** Asks macOS to surface the consent prompt for each grant set to true.
 *  The prompts are system dialogs answered outside this request. */
export interface ComputerPermissionRequest {
  accessibility?: boolean
  screen_recording?: boolean
}

export interface ComputerPermissionRequestResponse {
  status: string
  /** States observed right after asking; 'denied' means 'not granted yet', not 'refused'. */
  accessibility: ComputerPermissionState
  screen_recording: ComputerPermissionState
}

export type ComputerPermissionState = 'granted' | 'denied' | 'unknown'
export type ComputerBlocker = '' | 'disabled' | 'unsupported' | 'no_helper' | 'permissions'

export interface ComputerHelperStatus {
  installed: boolean
  connected: boolean
  version?: string
}

export interface ComputerStatusResponse {
  /** Server-authoritative platform support. Do not infer this from the browser. */
  supported: boolean
  /** GOOS of the jcode server, for example 'darwin', 'linux', or 'windows'. */
  platform: string
  /** Human-readable reason when `supported` is false. */
  reason?: string
  /** Canonical persisted config. The settings page must save this shape back. */
  config: ComputerConfig
  status: {
    enabled: boolean
    /** True only when the native helper and both required TCC grants are ready. */
    available: boolean
    /** The first shut gate: 'disabled' | 'unsupported' | 'no_helper' | 'permissions' | ''. */
    blocker: ComputerBlocker
    detail?: string
    max_batch: number
    /** Built-in tier per configured bundle id, so the UI never reimplements the
     *  rules in internal/computer/tiers.go. Only covers apps that have a config
     *  row; a freshly typed bundle id is absent until the config round-trips. */
    tiers?: Record<string, string>
    helper: ComputerHelperStatus
    accessibility: ComputerPermissionState
    screen_recording: ComputerPermissionState
  }
}
