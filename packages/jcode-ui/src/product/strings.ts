/**
 * Product-composer strings — every user-facing label the product composer
 * (ChatInput / WorkspacePicker / BranchPicker / GoalBanner) renders.
 *
 * The jcode-ui package deliberately does NOT depend on an i18n library: hosts
 * inject already-localized strings through `ProductComposerHost.strings`
 * (partial — merged over these English defaults). Fields that need
 * interpolation are functions, so plural/variable handling stays on the host
 * side (the jcode app maps these 1:1 onto its i18next `chat.*` / `goal.*` /
 * `branches.*` / `workspace.*` keys).
 */

export interface ProductComposerStrings {
  // ── composer textarea ──
  placeholder: string
  goalPlaceholder: string
  queuePlaceholder: string
  /** Fallback message body when only images are attached (no text). */
  attachedImages: string
  send: string
  queue: string
  stop: string
  stopAgent: string
  stopAndNext: string
  removeQueued: string
  add: string
  attachFiles: string
  command: string
  goal: string
  workflowBadge: string
  goalSlashDesc: string
  goalHintNext: string
  goalHintNextReplaces: string
  goalHintRemove: string
  goalHintReplace: string
  modelNoImages: string

  // ── mode picker ──
  modeApproval: string
  modeApprovalSub: string
  modePlan: string
  modePlanSub: string
  modeAuto: string
  modeAutoSub: string
  modeFullAccess: string
  modeFullAccessSub: string
  /** Shown in the mode picker when the host restricts `allowedModes` (M20). */
  modeCeilingHint: string

  // ── custom agent picker ──
  agentTitle: string
  agentDefault: string
  agentDefaultSub: string

  // ── model picker / manage dialog ──
  modelFilter: string
  modelCurrent: string
  modelFavorites: string
  modelReasoning: string
  modelTools: string
  modelImages: string
  modelNoImageInput: string
  modelNone: string
  modelNoMatch: string
  modelManage: string
  modelManageTitle: string
  modelToggleVisibility: string
  modelVisibleCount: (visible: number, total: number) => string
  effort: string
  effortTitle: string
  effortDefault: string

  // ── context-capacity popup ──
  contextTitle: string
  contextSystemPrompt: string
  contextSystemTools: string
  contextMcpTools: string
  contextSkills: string
  contextMessages: string
  contextInput: string
  contextOutput: string
  contextCached: string
  contextReasoning: string
  contextCacheHitRate: string

  // ── workspace picker ──
  workspaceNone: string
  workspaceNonePlural: string
  workspaceSearch: string
  workspaceLoading: string
  workspaceNoFolders: string
  workspaceOpen: string
  workspaceOpenFolder: string
  workspaceOpenError: string
  workspacePathPlaceholder: string
  remoteConnect: string

  // ── branch picker ──
  branchesTitle: string
  branchesNone: string
  branchSearch: string
  branchCreate: string
  branchCreateBtn: string
  branchNewName: string
  branchCurrent: (name: string) => string
  branchConfirmTitle: string
  branchConfirmIntro: (branch: string) => string
  branchConfirmMore: (count: number) => string
  branchConfirmStash: string
  branchConfirmDiscard: string
  branchConfirmCancel: string
  branchConfirmHint: string
  branchSwitchError: string

  // ── goal banner ──
  goalStatusActive: string
  goalStatusCompleted: string
  goalStatusBlocked: string
  goalStarted: string
  goalElapsed: string
  goalEdit: string
  goalEditTitle: string
  goalClear: string
  goalSaveFailed: string
  goalTokens: (used: number) => string
  goalTokensK: (k: string) => string
  durationSeconds: (n: number) => string
  durationMinutes: (m: number, s: number) => string
  durationHours: (h: number, m: number) => string

  // ── shared ──
  commonTokens: (used: string) => string
  commonLoading: string
  commonClose: string
  commonCancel: string
  commonSave: string
  commonDone: string
  commonEnable: string
  commonDisable: string
  commonRecommended: string
}

/**
 * English defaults — identical to web/src/i18n/locales/en.ts so a host that
 * injects nothing renders the stock jcode en UI.
 */
export const defaultProductComposerStrings: ProductComposerStrings = {
  placeholder: 'Ask JCODE or type / for commands',
  goalPlaceholder: 'Describe the goal — the agent pursues it until verifiably complete',
  queuePlaceholder: 'Agent is working — type to queue a follow-up…',
  attachedImages: '(see attached images)',
  send: 'Send',
  queue: 'Queue',
  stop: 'Stop',
  stopAgent: 'Stop agent (Esc)',
  stopAndNext: 'Stop current turn and send the next queued message',
  removeQueued: 'Remove from queue',
  add: 'Add',
  attachFiles: 'Attach files',
  command: 'Command',
  goal: 'Goal',
  workflowBadge: 'workflow',
  goalSlashDesc: 'Arm Goal mode — your next message becomes the objective',
  goalHintNext: 'Next message becomes the session goal',
  goalHintNextReplaces: 'Next message replaces the current goal',
  goalHintRemove: 'Remove goal',
  goalHintReplace: 'Setting a new goal replaces the current one',
  modelNoImages: 'Current model does not support images',

  modeApproval: 'Ask for approval',
  modeApprovalSub: 'Pause before each write or command.',
  modePlan: 'Plan',
  modePlanSub: 'Propose a plan first; nothing runs until you approve it.',
  modeAuto: 'Auto',
  modeAutoSub: 'AI reviewer allows safe tools; uncertain ones ask.',
  modeFullAccess: 'Full access',
  modeFullAccessSub: 'Act freely — no approval prompts.',
  modeCeilingHint: 'This host limits which modes are available',

  agentTitle: 'Agent',
  agentDefault: 'Default agent',
  agentDefaultSub: 'Use JCODE’s standard instructions and tools.',

  modelFilter: 'Filter models…',
  modelCurrent: 'Current',
  modelFavorites: '★ Favorites',
  modelReasoning: 'Reasoning',
  modelTools: 'Tool use',
  modelImages: 'Image input',
  modelNoImageInput: 'Current model does not support images',
  modelNone: 'No models available',
  modelNoMatch: 'No models match your filter.',
  modelManage: 'Manage models…',
  modelManageTitle: 'Manage Models',
  modelToggleVisibility: 'Toggle which models appear in the model selector',
  modelVisibleCount: (visible, total) => `${visible} of ${total} models visible in the selector`,
  effort: 'Effort',
  effortTitle: 'Reasoning effort for this model',
  effortDefault: 'Default',

  contextTitle: 'Context capacity',
  contextSystemPrompt: 'System prompt',
  contextSystemTools: 'System tools',
  contextMcpTools: 'MCP tools',
  contextSkills: 'Skills',
  contextMessages: 'Messages',
  contextInput: 'Input',
  contextOutput: 'Output',
  contextCached: 'Cached',
  contextReasoning: 'Reasoning',
  contextCacheHitRate: 'Cache hit rate',

  workspaceNone: 'No workspace',
  workspaceNonePlural: 'No workspaces',
  workspaceSearch: 'Search workspaces',
  workspaceLoading: 'Loading…',
  workspaceNoFolders: 'No folders',
  workspaceOpen: 'Open',
  workspaceOpenFolder: 'Open folder',
  workspaceOpenError: 'Failed to open workspace',
  workspacePathPlaceholder: '/path/to/folder',
  remoteConnect: 'Remote connect',

  branchesTitle: 'Branches',
  branchesNone: 'No branches',
  branchSearch: 'Search branches',
  branchCreate: 'Create & checkout new branch',
  branchCreateBtn: 'Create',
  branchNewName: 'new-branch-name',
  branchCurrent: (name) => `Branch: ${name}`,
  branchConfirmTitle: 'Uncommitted work',
  branchConfirmIntro: (branch) => `Switching to "${branch}" would overwrite local files at:`,
  branchConfirmMore: (count) => `+${count} more`,
  branchConfirmStash: 'Stash & switch',
  branchConfirmDiscard: 'Discard & switch',
  branchConfirmCancel: 'Cancel',
  branchConfirmHint:
    'Stash saves your work (recover with git stash pop). Discard permanently deletes these files, including untracked ones.',
  branchSwitchError: 'Failed to switch branch',

  goalStatusActive: 'Active',
  goalStatusCompleted: 'Completed',
  goalStatusBlocked: 'Blocked',
  goalStarted: 'Started',
  goalElapsed: 'Elapsed',
  goalEdit: 'Edit',
  goalEditTitle: 'Edit goal',
  goalClear: 'Clear goal',
  goalSaveFailed: 'Could not save the goal',
  goalTokens: (used) => `${used} tokens`,
  goalTokensK: (k) => `${k}k tokens`,
  durationSeconds: (n) => `${n}s`,
  durationMinutes: (m, s) => `${m}m ${s}s`,
  durationHours: (h, m) => `${h}h ${m}m`,

  commonTokens: (used) => `${used} tokens`,
  commonLoading: 'Loading…',
  commonClose: 'Close',
  commonCancel: 'Cancel',
  commonSave: 'Save',
  commonDone: 'Done',
  commonEnable: 'Enable',
  commonDisable: 'Disable',
  commonRecommended: 'Recommended',
}

/** Merge host overrides over the English defaults. */
export function resolveProductComposerStrings(
  overrides?: Partial<ProductComposerStrings>,
): ProductComposerStrings {
  return overrides ? { ...defaultProductComposerStrings, ...overrides } : defaultProductComposerStrings
}
