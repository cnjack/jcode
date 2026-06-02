// API client for jcode backend
import type { ModelsResponse, AgentMode, ExecResponse, DiffResponse, MCPListResponse, BrowseResponse, SSHListResponse, SkillInfo, SlashCommandInfo, TodoItem, SessionItem, SessionEntry, FileItem, SetupProvider, SetupModel, ProviderDetail, ModelStateResponse, ChatImage } from '@/types/api'

const BASE = ''

async function request<T>(path: string, options?: RequestInit): Promise<T> {
  const resp = await fetch(`${BASE}${path}`, {
    ...options,
    headers: {
      'Content-Type': 'application/json',
      ...options?.headers,
    },
  })
  if (!resp.ok) {
    const body = await resp.json().catch(() => ({ error: resp.statusText }))
    throw new Error(body.error || `HTTP ${resp.status}`)
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
    request<{ status: string; version: string; pwd: string; provider: string; model: string; mode: string; session_id: string; running: boolean; image_support?: boolean; needs_setup?: boolean }>(
      '/api/health',
    ),
  status: () =>
    request<{
      running: boolean
      clients: number
      pwd: string
      provider: string
      model: string
      mode: string
    }>('/api/status'),
  config: () => request<{ provider: string; model: string; max_iterations: number }>('/api/config'),
  todos: () => request<TodoItem[]>('/api/todos'),
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
  approval: (id: string, approved: boolean) =>
    request<{ status: string }>('/api/approval', {
      method: 'POST',
      body: JSON.stringify({ id, approved }),
    }),
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
  mcpList: () => request<MCPListResponse>('/api/mcp'),
  mcpToggle: (name: string, enabled: boolean) =>
    request<{ status: string }>(`/api/mcp/${encodeURIComponent(name)}/toggle`, {
      method: 'POST',
      body: JSON.stringify({ enabled }),
    }),
  browse: (path?: string) => {
    const q = path ? `?path=${encodeURIComponent(path)}` : ''
    return request<BrowseResponse>(`/api/browse${q}`)
  },
  switchProject: (path: string) =>
    request<{ status: string; pwd: string }>('/api/project/switch', {
      method: 'POST',
      body: JSON.stringify({ path }),
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
  stop: () =>
    request<{ status: string }>('/api/stop', { method: 'POST' }),
  sshList: () =>
    request<SSHListResponse>('/api/ssh'),
  skillsList: () =>
    request<SkillInfo[]>('/api/skills'),
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

  // Setup API
  setupProviders: () =>
    request<SetupProvider[]>('/api/setup/providers'),
  setupProviderModels: (providerId: string) =>
    request<SetupModel[]>(`/api/setup/providers/${encodeURIComponent(providerId)}/models`),
  setupComplete: (data: { provider: string; model: string; api_key: string; base_url?: string }) =>
    request<{ status: string; provider: string; model: string }>('/api/setup/complete', {
      method: 'POST',
      body: JSON.stringify(data),
    }),
  setupStatus: () =>
    request<{ needs_setup: boolean }>('/api/setup/status'),
  setupValidate: (data: { provider: string; api_key: string; base_url?: string }) =>
    request<{ valid: boolean; error?: string }>('/api/setup/validate', {
      method: 'POST',
      body: JSON.stringify(data),
    }),

  // Provider management
  listProviders: () =>
    request<ProviderDetail[]>('/api/providers'),
  addProvider: (data: { id: string; api_key: string; base_url?: string }) =>
    request<{ status: string }>('/api/providers', {
      method: 'POST',
      body: JSON.stringify(data),
    }),
  deleteProvider: (id: string) =>
    request<{ status: string }>(`/api/providers/${encodeURIComponent(id)}`, { method: 'DELETE' }),

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
}
