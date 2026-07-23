/**
 * Product-composer types — mirror the jcode web backend's API shapes.
 *
 * These were lifted verbatim from web/src/lib/types.ts so the product composer
 * (ChatInput / WorkspacePicker / BranchPicker / GoalBanner) can live in the
 * shared package without importing the app's type module. Keep the field names
 * snake_case: they cross the HTTP boundary.
 */

/** Session approval mode (unified across transports). */
export type AgentMode = 'approval' | 'plan' | 'auto' | 'full_access'

/** ReasoningOption mirrors models.dev's reasoning_options. */
export interface ReasoningOption {
  type: string
  values?: string[]
  min?: number
  max?: number
}

export interface ModelInfo {
  id: string
  name: string
  tool_call: boolean
  context_limit?: number
  reasoning?: boolean
  recommended?: boolean
  default_enabled?: boolean
  /** Whether this model is available in the chat picker. Omitted is treated as disabled. */
  enabled?: boolean
  image_support?: boolean
  /** How this model exposes its reasoning/thinking controls. */
  reasoning_options?: ReasoningOption[]
}

export interface ProviderInfo {
  id: string
  name: string
  /** true for user-configured OpenAI-compatible providers. */
  custom?: boolean
  models: ModelInfo[]
}

export interface ModelRef {
  provider: string
  model: string
}

/** Unified slash command (built-in + skill + flow). */
export interface SlashCommandInfo {
  slash: string
  description: string
  type: 'builtin' | 'skill' | 'flow'
}

// ─── Task stats (context-capacity popup) ────────────────────────────────────

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

// ─── Workspace ──────────────────────────────────────────────────────────────

/** Minimal task shape the workspace picker consumes (it only reads `project`). */
export interface WorkspaceTaskRef {
  uuid: string
  project: string
}

export interface BrowseFolder {
  name: string
  path: string
}

export interface BrowseResult {
  current: string
  folders: BrowseFolder[]
}

// ─── Git branches ───────────────────────────────────────────────────────────

export interface GitBranchesResult {
  /** Empty if not a git repo. */
  current: string
  /** Local branches, most-recently-committed first. */
  branches: string[]
}

export interface GitCheckoutResult {
  /** New current branch on success; '' when blocked. */
  branch: string
  /** true when a plain switch was aborted by uncommitted changes. */
  blocked?: boolean
  message?: string
  /** Files that would be overwritten, parsed from git's output. */
  files?: string[]
  stashed?: boolean
}

// ─── Remote workspaces ──────────────────────────────────────────────────────

export type RemoteKind = 'ssh' | 'docker'

export interface RemoteMeta {
  /** defaults to 'ssh' for back-compat. */
  kind?: RemoteKind
  /** host:port as dialed (ssh). */
  host: string
  user: string
  port: number
  remotePath: string
  /** docker container name/id. */
  container?: string
}

export type RemotePrefill = RemoteMeta & { loadTaskUuid?: string }
