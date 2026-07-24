/**
 * ProductComposerHost — the seam between the product composer components and
 * the host application's state/backend layers.
 *
 * The composer components (ChatInput / WorkspacePicker / BranchPicker /
 * GoalBanner) are free of Redux, fetch, Tauri, and i18next imports: everything
 * they need arrives through this interface. The jcode desktop app implements it
 * by projecting its RTK store + `/api/*` client (see web/src/app/composerHost.ts);
 * other hosts (console/mobile) implement the same surface over their own stack.
 *
 * What is NOT here (and why):
 *   - send / enqueue / stop / queued / tokenSnapshot / goal / todos — these come
 *     from the jcode-ui-core ChatRuntime (`RuntimeProvider`), which the composer
 *     reads via `useRuntimeState` / `useRuntimeActions`, exactly like Thread.
 *   - goal submission on send — that is a property of the runtime's sendMessage
 *     action (the host decides that an armed goal turns the next text into
 *     `POST /api/goal {objective, start: true}`); the composer only toggles the
 *     `goalArmed` flag via `setGoalArmed`.
 */

import type { Goal } from 'jcode-ui-core'
import type {
  AgentMode,
  BrowseResult,
  CustomAgentInfo,
  GitBranchesResult,
  GitCheckoutResult,
  ModelRef,
  ProviderInfo,
  RemotePrefill,
  SlashCommandInfo,
  TaskStats,
  WorkspaceTaskRef,
} from './types.js'
import type { ProductComposerStrings } from './strings.js'

export interface ProductComposerHost {
  // ── Model catalog state ───────────────────────────────────────────────────
  providerName: string
  modelName: string
  mode: AgentMode
  /**
   * Modes the host allows in the mode picker, in pick order. Absent ⇒ all
   * four modes (the desktop default). Cloud hosts cap cloud-originated
   * sessions at auto (M20), so they pass `['approval', 'plan', 'auto']` and
   * a `modeCeilingHint` string explaining the ceiling.
   */
  allowedModes?: AgentMode[]
  providers: ProviderInfo[]
  /** Favorite refs as "provider/model" keys. */
  favoriteModels: string[]
  recentModels: ModelRef[]
  /** Whether the current model accepts image input (paste/attach gating). */
  imageSupport: boolean
  /** Per-"provider/model" reasoning-effort overrides. */
  effortOverrides: Record<string, string>
  /** Available top-level custom agents. Empty hides the agent picker. */
  agents?: CustomAgentInfo[]
  /** Selected custom agent name. Empty means the built-in Default agent. */
  agentName?: string

  // ── Chat state ────────────────────────────────────────────────────────────
  slashCommands: SlashCommandInfo[]
  /** True when the conversation has any timeline items (welcome ↔ docked layout). */
  hasMessages: boolean
  goalArmed: boolean
  /** Current conversation id — keys composer drafts and task-stats lookups. */
  sessionId: string

  // ── Workspace state ───────────────────────────────────────────────────────
  projectPath: string
  /** All known tasks; the workspace picker derives the workspace list from `project`. */
  tasks: WorkspaceTaskRef[]

  // ── Presentation ──────────────────────────────────────────────────────────
  /** Localized labels; merged over the built-in English defaults. */
  strings?: Partial<ProductComposerStrings>
  /** Resolve a provider id to an inline SVG string (null ⇒ initial-letter fallback). */
  resolveProviderIcon?: (provider: string, custom?: boolean) => string | null

  // ── Model actions ─────────────────────────────────────────────────────────
  /** Switch the active model (host persists + updates its store). */
  selectModel: (provider: string, model: string) => void | Promise<void>
  /** Switch the session approval mode. */
  selectMode: (mode: AgentMode) => void | Promise<void>
  /** Select a top-level custom agent; empty string restores Default. */
  selectAgent?: (name: string) => void | Promise<void>
  /** Set/clear (empty string) a per-model reasoning-effort override. */
  setEffort: (provider: string, model: string, effort: string) => void | Promise<void>
  /** Toggle a favorite; the host knows the resulting state. */
  toggleFavorite: (provider: string, model: string) => void | Promise<void>
  /** Enable/disable a model in the picker (Manage Models dialog). */
  setModelEnabled: (provider: string, model: string, enabled: boolean) => void | Promise<void>
  /** Re-hydrate the model catalog after the Manage dialog closes. */
  refreshModels: () => void | Promise<void>

  // ── Chat actions ──────────────────────────────────────────────────────────
  setGoalArmed: (armed: boolean) => void
  /** Context-capacity popup data; null when unavailable. */
  fetchTaskStats: (sessionId: string) => Promise<TaskStats | null>

  // ── Workspace actions ─────────────────────────────────────────────────────
  /** Return the subset of local paths that no longer exist on disk. */
  validateWorkspacePaths: (paths: string[]) => Promise<string[]>
  browseFolders: (path?: string) => Promise<BrowseResult>
  /**
   * Activate a workspace. When `path` equals the current project the host starts
   * a NEW session in place; otherwise it switches projects. Throws on failure
   * (the picker shows the error inline).
   */
  switchWorkspace: (path: string) => Promise<void>
  /**
   * Desktop-native folder picker. Absent ⇒ the picker only offers the in-app
   * folder browser. A rejection falls back to the in-app browser; null = user
   * cancelled.
   */
  pickFolder?: (defaultPath?: string) => Promise<string | null>
  /** Open the host's remote-connect wizard (ssh:// and docker:// workspaces). */
  openRemoteConnect?: (prefill?: RemotePrefill | null) => void

  // ── Branch actions ────────────────────────────────────────────────────────
  fetchBranches: () => Promise<GitBranchesResult>
  checkoutBranch: (branch: string, create: boolean, strategy: '' | 'stash' | 'force') => Promise<GitCheckoutResult>

  // ── Goal actions (GoalBanner) ─────────────────────────────────────────────
  /** start=true kicks off an agent run; start=false only edits the objective. */
  setGoal: (objective: string, start: boolean) => Promise<Goal>
  /** Clear the active goal. Must clear local state even when the backend call fails. */
  clearGoal: () => Promise<void>
}
