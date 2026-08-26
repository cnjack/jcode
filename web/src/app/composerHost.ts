/**
 * Product-composer host — projects the RTK store + `/api/*` client into the
 * `ProductComposerHost` interface consumed by `jcode-ui/product` (ChatInput,
 * WorkspacePicker, BranchPicker, GoalBanner).
 *
 * Every action mirrors the exact call sequence the pre-extraction components
 * ran inline (API first, then store updates, same optimistic/revert order), so
 * desktop behavior is unchanged; only the location of the code moved.
 */

import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import type { ProductComposerHost, ProductComposerStrings } from 'jcode-ui/product'
import { useAppSelector } from './hooks'
import { store } from './store'
import type { RootState } from './store'
import {
  chatActions,
  modelActions,
  sessionActions,
  loadWorkspaceState,
  startScratchChat,
} from './store'
import { api } from '../lib/api'
import { iconForProvider } from '../lib/providerIcons'
import { openRemoteConnect } from '../lib/remote'
import { isTauri, pickFolder } from '../lib/useDesktop'
import type { AgentMode } from '../lib/types'

function buildStrings(t: (key: string, opts?: Record<string, unknown>) => string): ProductComposerStrings {
  return {
    placeholder: t('chat.placeholder'),
    goalPlaceholder: t('chat.goalPlaceholder'),
    queuePlaceholder: t('chat.queuePlaceholder'),
    attachedImages: t('chat.attachedImages'),
    send: t('chat.send'),
    queue: t('chat.queue'),
    stop: t('chat.stop'),
    stopAgent: t('chat.stopAgent'),
    stopAndNext: t('chat.stopAndNext'),
    removeQueued: t('chat.removeQueued'),
    add: t('chat.add'),
    attachFiles: t('chat.attachFiles'),
    command: t('chat.command'),
    goal: t('chat.goal'),
    workflowBadge: t('chat.workflowBadge'),
    goalSlashDesc: t('chat.goalSlashDesc'),
    goalHintNext: t('chat.goalHint.next'),
    goalHintNextReplaces: t('chat.goalHint.nextReplaces'),
    goalHintRemove: t('chat.goalHint.remove'),
    goalHintReplace: t('chat.goalHint.replace'),
    modelNoImages: t('chat.model.noImages'),

    modeApproval: t('chat.modes.approval'),
    modeApprovalSub: t('chat.modes.approvalSub'),
    modePlan: t('chat.modes.plan'),
    modePlanSub: t('chat.modes.planSub'),
    modeAuto: t('chat.modes.auto'),
    modeAutoSub: t('chat.modes.autoSub'),
    modeFullAccess: t('chat.modes.fullAccess'),
    modeFullAccessSub: t('chat.modes.fullAccessSub'),
    modeCeilingHint: t('chat.modes.ceilingHint'),

    agentTitle: t('chat.agent.title'),
    agentDefault: t('chat.agent.default'),
    agentDefaultSub: t('chat.agent.defaultSub'),

    modelFilter: t('chat.model.filter'),
    modelCurrent: t('chat.model.current'),
    modelFavorites: t('chat.model.favorites'),
    modelReasoning: t('chat.model.reasoning'),
    modelTools: t('chat.model.tools'),
    modelImages: t('chat.model.images'),
    modelImageOutput: t('chat.model.imageOutput'),
    modelNoImageInput: t('chat.model.noImageInput'),
    modelNone: t('chat.model.none'),
    modelNoMatch: t('chat.model.noMatch'),
    modelManage: t('chat.model.manage'),
    modelManageTitle: t('chat.model.manageTitle'),
    modelToggleVisibility: t('chat.model.toggleVisibility'),
    modelVisibleCount: (visible, total) => t('chat.model.visibleCount', { visible, total }),
    effort: t('chat.model.effort'),
    effortTitle: t('chat.model.effortTitle'),
    effortDefault: t('chat.model.effortDefault'),

    contextTitle: t('contextCapacity.title'),
    contextSystemPrompt: t('contextCapacity.systemPrompt'),
    contextSystemTools: t('contextCapacity.systemTools'),
    contextMcpTools: t('contextCapacity.mcpTools'),
    contextSkills: t('contextCapacity.skills'),
    contextMessages: t('contextCapacity.messages'),
    contextInput: t('contextCapacity.input'),
    contextOutput: t('contextCapacity.output'),
    contextCached: t('contextCapacity.cached'),
    contextReasoning: t('contextCapacity.reasoning'),
    contextCacheHitRate: t('contextCapacity.cacheHitRate'),

    workspaceNone: t('workspace.none'),
    workspaceNonePlural: t('workspace.nonePlural'),
    workspaceSearch: t('workspace.search'),
    workspaceLoading: t('workspace.loading'),
    workspaceNoFolders: t('workspace.noFolders'),
    workspaceOpen: t('workspace.open'),
    workspaceOpenFolder: t('workspace.openFolder'),
    workspaceOpenError: t('workspace.openError'),
    workspacePathPlaceholder: t('projectSwitcher.pathPlaceholder'),
    workspaceScratchAction: t('workspace.workWithoutProject'),
    remoteConnect: t('nav.remoteConnect'),

    branchesTitle: t('branches.title'),
    branchesNone: t('branches.none'),
    branchSearch: t('branches.search'),
    branchCreate: t('branches.create'),
    branchCreateBtn: t('branches.createBtn'),
    branchNewName: t('branches.newName'),
    branchCurrent: (name) => t('branches.current').replace('{name}', name),
    branchConfirmTitle: t('branches.confirmTitle'),
    branchConfirmIntro: (branch) => t('branches.confirmIntro').replace('{branch}', branch),
    branchConfirmMore: (count) => t('branches.confirmMore').replace('{count}', String(count)),
    branchConfirmStash: t('branches.confirmStash'),
    branchConfirmDiscard: t('branches.confirmDiscard'),
    branchConfirmCancel: t('branches.confirmCancel'),
    branchConfirmHint: t('branches.confirmHint'),
    branchSwitchError: t('errors.branchSwitch'),

    goalStatusActive: t('goal.status.active'),
    goalStatusCompleted: t('goal.status.completed'),
    goalStatusBlocked: t('goal.status.blocked'),
    goalStarted: t('goal.started'),
    goalElapsed: t('goal.elapsed'),
    goalEdit: t('goal.edit'),
    goalEditTitle: t('goal.editTitle'),
    goalClear: t('goal.clearGoal'),
    goalSaveFailed: t('goal.saveFailed'),
    goalTokens: (used) => t('goal.tokens', { used }),
    goalTokensK: (k) => t('goal.tokensK', { k }),
    durationSeconds: (n) => t('chat.durationSeconds', { n }),
    durationMinutes: (m, s) => t('chat.durationMinutes', { m, s }),
    durationHours: (h, m) => t('goal.durationHours', { h, m }),

    commonTokens: (used) => t('common.tokens', { used }),
    commonLoading: t('common.loading'),
    commonClose: t('common.close'),
    commonCancel: t('common.cancel'),
    commonSave: t('common.save'),
    commonDone: t('common.done'),
    commonEnable: t('common.enable'),
    commonDisable: t('common.disable'),
    commonRecommended: t('common.recommended'),
  }
}

/** Stable action bag — closes over the store singleton, so it never changes. */
function assertForegroundTask(taskID: string): void {
  if (store.getState().session.currentSessionId === taskID) return
  const error = new Error('Task changed while the request was in flight')
  error.name = 'AbortError'
  throw error
}

export const productComposerActions = {
  async selectModel(provider: string, model: string) {
    const taskID = store.getState().session.currentSessionId || undefined
    try {
      await api.switchModel(provider, model, taskID)
      if (store.getState().session.currentSessionId !== (taskID || '')) return
      store.dispatch(modelActions.setProvider(provider))
      store.dispatch(modelActions.setModel(model))
    } catch {
      /* ignore — the next status poll reconciles */
    }
  },

  async selectMode(next: AgentMode) {
    const taskID = store.getState().session.currentSessionId || undefined
    try {
      await api.switchMode(next, taskID)
    } catch {
      /* ignore */
    }
    if (store.getState().session.currentSessionId !== (taskID || '')) return
    store.dispatch(modelActions.setMode(next))
  },

  async selectAgent(name: string) {
    const taskID = store.getState().session.currentSessionId || undefined
    try {
      const result = await api.switchAgent(name, taskID)
      if (store.getState().session.currentSessionId !== (taskID || '')) return
      store.dispatch(modelActions.setAgent(result.agent || ''))
      if (result.provider) store.dispatch(modelActions.setProvider(result.provider))
      if (result.model) store.dispatch(modelActions.setModel(result.model))
    } catch {
      /* ignore — the next status poll reconciles */
    }
  },

  async setEffort(provider: string, model: string, effort: string) {
    const key = `${provider}/${model}`
    const prev = (store.getState() as RootState).model.effortOverrides[key] ?? ''
    store.dispatch(modelActions.setEffortOverride({ provider, model, effort }))
    try {
      await api.setModelEffort(provider, model, effort)
    } catch {
      store.dispatch(modelActions.setEffortOverride({ provider, model, effort: prev }))
    }
  },

  async toggleFavorite(provider: string, model: string) {
    try {
      const result = await api.toggleFavorite(provider, model)
      store.dispatch(modelActions.setFavorite({ provider, model, favorite: result.favorite }))
    } catch {
      /* ignore */
    }
  },

  async setModelEnabled(provider: string, model: string, enabled: boolean) {
    try {
      await api.toggleModelEnabled(provider, model, enabled)
      // Reflect locally so the Manage dialog toggles immediately.
      const { providers } = (store.getState() as RootState).model
      const nextProviders = providers.map((p) =>
        p.id === provider
          ? { ...p, models: p.models.map((m) => (m.id === model ? { ...m, enabled } : m)) }
          : p,
      )
      store.dispatch(modelActions.setProviders(nextProviders))
    } catch {
      /* ignore */
    }
  },

  async refreshModels() {
    const taskID = store.getState().session.currentSessionId || undefined
    try {
      const resp = await api.models(taskID)
      if (store.getState().session.currentSessionId !== (taskID || '')) return
      store.dispatch(modelActions.setProviders(resp.providers))
    } catch {
      /* ignore */
    }
  },

  setGoalArmed(armed: boolean) {
    store.dispatch(chatActions.setGoalArmed(armed))
  },

  async fetchTaskStats(sessionId: string) {
    try {
      return await api.taskStats(sessionId)
    } catch {
      return null
    }
  },

  async validateWorkspacePaths(paths: string[]) {
    const res = await api.validatePaths(paths)
    return res.missing || []
  },

  browseFolders(path?: string) {
    return api.browse(path)
  },

  async switchWorkspace(path: string) {
    const state = store.getState() as RootState
    if (path === state.session.projectPath) {
      const resp = await api.newSession(undefined, undefined, state.session.workspaceKind)
      store.dispatch(chatActions.clearChat())
      store.dispatch(sessionActions.setCurrentSession(resp.session_id))
      store.dispatch(sessionActions.setProjectPath(resp.project || resp.workspace_key || resp.pwd || path))
      store.dispatch(sessionActions.setWorkspaceKind(resp.workspace_kind))
      await store.dispatch(loadWorkspaceState())
      return
    }
    const resp = await api.switchProject(path)
    store.dispatch(chatActions.clearChat())
    store.dispatch(sessionActions.setCurrentSession(''))
    store.dispatch(sessionActions.setProjectPath(resp.pwd || path))
    store.dispatch(sessionActions.setWorkspaceKind(resp.workspace_kind))
    await store.dispatch(loadWorkspaceState())
  },

  async startScratchWorkspace() {
    await store.dispatch(startScratchChat()).unwrap()
  },

  openRemoteConnect,

  async fetchBranches() {
    const taskID = store.getState().session.currentSessionId
    const result = await api.gitBranches(taskID || undefined)
    assertForegroundTask(taskID)
    return result
  },

  async checkoutBranch(branch: string, create: boolean, strategy: '' | 'stash' | 'force') {
    const taskID = store.getState().session.currentSessionId
    const result = await api.gitCheckout(branch, create, strategy, taskID || undefined)
    assertForegroundTask(taskID)
    return result
  },

  async setGoal(objective: string, start: boolean) {
    const taskID = store.getState().session.currentSessionId || undefined
    const goal = await api.setGoal(objective, start, taskID)
    if (store.getState().session.currentSessionId !== (taskID || '')) return goal
    store.dispatch(chatActions.setGoal(goal))
    return goal
  },

  async clearGoal() {
    const taskID = store.getState().session.currentSessionId || undefined
    try {
      await api.clearGoal(taskID)
    } catch {
      // still clear local so the banner dismisses
    }
    if (store.getState().session.currentSessionId !== (taskID || '')) return
    store.dispatch(chatActions.setGoal(null))
  },
}

/**
 * Compose the host object. Memoized on the selected state slices so the host
 * identity is stable between unrelated renders (the pickers key effects off
 * it, mirroring the pre-extraction selector dependencies).
 */
export function useProductComposerHost(): ProductComposerHost {
  const { t } = useTranslation()

  const providerName = useAppSelector((s) => s.model.providerName)
  const modelName = useAppSelector((s) => s.model.modelName)
  const mode = useAppSelector((s) => s.model.mode)
  const providers = useAppSelector((s) => s.model.providers)
  const favoriteModels = useAppSelector((s) => s.model.favoriteModels)
  const recentModels = useAppSelector((s) => s.model.recentModels)
  const imageSupport = useAppSelector((s) => s.model.imageSupport)
  const effortOverrides = useAppSelector((s) => s.model.effortOverrides)
  const agents = useAppSelector((s) => s.model.agents)
  const agentName = useAppSelector((s) => s.model.agentName)
  const slashCommands = useAppSelector((s) => s.chat.slashCommands)
  const hasMessages = useAppSelector((s) => s.chat.timeline.length > 0)
  const goalArmed = useAppSelector((s) => s.chat.goalArmed)
  const sessionId = useAppSelector((s) => s.session.currentSessionId)
  const projectPath = useAppSelector((s) => s.session.projectPath)
  const workspaceKind = useAppSelector((s) => s.session.workspaceKind)
  const tasks = useAppSelector((s) => s.session.tasks)

  const strings = useMemo(() => buildStrings(t), [t])

  return useMemo(
    () => ({
      providerName,
      modelName,
      mode,
      providers,
      favoriteModels,
      recentModels,
      imageSupport,
      effortOverrides,
      agents,
      agentName,
      slashCommands,
      hasMessages,
      goalArmed,
      sessionId,
      projectPath,
      workspaceKind,
      tasks,
      strings,
      resolveProviderIcon: iconForProvider,
      pickFolder: isTauri ? pickFolder : undefined,
      ...productComposerActions,
    }),
    [
      providerName, modelName, mode, providers, favoriteModels, recentModels,
      imageSupport, effortOverrides, agents, agentName, slashCommands, hasMessages, goalArmed,
      sessionId, projectPath, workspaceKind, tasks, strings,
    ],
  )
}
