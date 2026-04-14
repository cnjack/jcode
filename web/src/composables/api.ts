// API client for jcode backend
import type { ModelsResponse, AgentMode, ExecResponse, DiffResponse, MCPListResponse, BrowseResponse } from '@/types/api'

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
  health: () =>
    request<{ status: string; version: string; pwd: string; provider: string; model: string; mode: string }>(
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
  todos: () => request<any[]>('/api/todos'),
  sessions: () => request<any[]>('/api/sessions'),
  session: (id: string) => request<any[]>(`/api/sessions/${encodeURIComponent(id)}`),
  deleteSession: (id: string) =>
    request<{ status: string }>(`/api/sessions/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  newSession: () => request<{ status: string; session_id: string }>('/api/sessions', { method: 'POST' }),
  files: (path?: string) => {
    const q = path ? `?path=${encodeURIComponent(path)}` : ''
    return request<any[]>(`/api/files${q}`)
  },
  fileContent: (path: string) =>
    request<{ path: string; content: string }>(`/api/files/content?path=${encodeURIComponent(path)}`),
  chat: (message: string, mode?: AgentMode) =>
    request<{ status: string }>('/api/chat', {
      method: 'POST',
      body: JSON.stringify({ message, mode }),
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
  switchMode: (mode: AgentMode) =>
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
}
