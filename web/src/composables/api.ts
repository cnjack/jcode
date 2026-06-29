// API client for jcode backend
import type { ModelsResponse, AgentMode, ExecResponse, DiffResponse, WorkspaceInfo, GitBranchesResponse, GitCheckoutResponse, TaskItem, TaskMetaPatch, MCPListResponse, MCPServerRequest, MCPLoginStatus, BrowseResponse, SSHListResponse, SkillInfo, SlashCommandInfo, TodoItem, Goal, SessionItem, SessionEntry, FileItem, SetupProvider, SetupModel, ProviderDetail, ProviderAdvanced, CustomModelDetail, ValidateResult, CatalogModel, ModelStateResponse, ChatImage, AskUserAnswer, AskUserRequestData, ApprovalRequestData, RemoteConnectRequest, RemoteConnectResponse, RemoteListDirResponse, RemoteBindResponse, DockerContainersResponse, UsageStats, TaskStats, TokenUpdateData } from '@/types/api'
import type { AutomationItem, AutomationRun, AutomationTemplate, AutomationCreate, Automation } from '@/types/automation'
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
  config: () => request<{ provider: string; model: string; max_iterations: number }>('/api/config'),
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
    request<{ enabled: boolean }>('/api/channel/ble'),
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
  addProvider: (data: { id: string; api_key: string; name?: string; model?: string; model_reasoning?: boolean; vision?: boolean; thinking?: boolean; reasoning_effort?: string } & ProviderAdvanced) =>
    request<{ status: string }>('/api/providers', {
      method: 'POST',
      body: JSON.stringify(data),
    }),
  updateProvider: (id: string, data: { api_key?: string; name?: string; custom_models?: CustomModelDetail[]; vision?: boolean; thinking?: boolean; reasoning_effort?: string } & ProviderAdvanced) =>
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
}
