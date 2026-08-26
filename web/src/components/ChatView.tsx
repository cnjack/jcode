/**
 * ChatView — the main chat column.
 *
 * Two states (matches Vue App.vue):
 *   - Welcome: no messages → centered hero ([ J CODE ]) + elevated composer.
 *   - Conversation: messages exist → feathered scrollable timeline + docked
 *     composer (no hard border-t; messages dissolve into the surface).
 *
 * The TopBar (floated top-right) carries the panel menu + connection status,
 * so there is NO separate header here.
 */

import { AskUserCard, Thread } from 'jcode-ui'
import { GoalBanner, ChatInput } from 'jcode-ui/product'
import { useTranslation } from 'react-i18next'
import { useAppSelector } from '../app/hooks'
import { useProductComposerHost } from '../app/composerHost'
import kimiBackground from '../assets/kimi-light-background.webp'
import zhipuBackground from '../assets/zhipu-light-background.webp'
import { ConversationLoadingView } from './ConversationLoadingView'
import { RemoteConnectionNotice } from './RemoteConnectionNotice'

const MODEL_BACKGROUNDS = {
  kimi: kimiBackground,
  zhipu: zhipuBackground,
} as const

type ModelBackdropKind = keyof typeof MODEL_BACKGROUNDS

export interface ChatViewProps {
  /** Read-only mode (automation-run replay): no composer, no follow. */
  readOnly?: boolean
}

/** Pending ("Thinking…") row — breathing ring + i18n label. The library's
 *  default is English-only; we override it here so the label follows the
 *  active locale (chat.thinking). Ring style lives in jcode-ui's CSS.
 *
 *  Rendered as `<PendingIndicator />` (NOT called as a render function) so
 *  its useTranslation hook stays on its own fiber — calling it directly as
 *  renderPending() would attach the hook to VirtualizedThread and trip the
 *  Rules of Hooks when the pending slot mounts/unmounts. */
function PendingIndicator() {
  const { t } = useTranslation()
  const label = t('chat.thinking')
  return (
    <div
      className="jcode-pending jcode-chat-col"
      role="status"
      aria-live="polite"
      aria-label={label}
    >
      <div className="jcode-pending__inner jcode-gutter">
        <span className="jcode-pending__ring" aria-hidden="true">
          <span className="jcode-pending-ring" />
        </span>
        <span className="jcode-pending__label">{label}</span>
      </div>
    </div>
  )
}

export function ChatView({ readOnly }: ChatViewProps) {
  const { t } = useTranslation()
  const turnDurationLabel = (durationMs: number) => {
    const totalSeconds = Math.max(0, Math.round(durationMs / 1000))
    const hours = Math.floor(totalSeconds / 3600)
    const minutes = Math.floor((totalSeconds % 3600) / 60)
    const seconds = totalSeconds % 60
    const duration = hours > 0
      ? t('chat.durationHours', { h: hours, m: minutes, s: seconds })
      : minutes > 0
        ? t('chat.durationMinutes', { m: minutes, s: seconds })
        : t('chat.durationSeconds', { n: seconds })
    return `${t('chat.turnDuration')} ${duration}`
  }
  const turnStrings = {
    turnDurationLabel,
    turnExpandLabel: t('chat.turnShowWork'),
    turnCollapseLabel: t('chat.turnHideWork'),
  }
  const host = useProductComposerHost()
  const hasMessages = useAppSelector((s) => s.chat.timeline.length > 0)
  const sessionLoading = useAppSelector((s) => s.chat.sessionLoading)
  const conversationLoadPhase = useAppSelector((s) => s.conversationLoad.phase)
  const pendingAskUser = useAppSelector((s) => {
    for (const item of s.chat.timeline) {
      if (
        item.kind === 'tool' &&
        item.data.name === 'ask_user' &&
        !!item.data.askUserId &&
        item.data.status === 'running' &&
        !item.data.output
      ) {
        return item.data
      }
    }
    return null
  })
  const projectPath = useAppSelector((s) => s.session.projectPath)
  const workspaceKind = useAppSelector((s) => s.session.workspaceKind)
  const backdropKind = useAppSelector((s) => {
    const provider = s.model.providers.find((candidate) => candidate.id === s.model.providerName)
    const model = provider?.models.find((candidate) => candidate.id === s.model.modelName)
    return modelBackdropKind([provider?.kind, s.model.providerName, provider?.name, s.model.modelName, model?.name])
  })
  const project = workspaceKind === 'scratch'
    ? t('workspace.noProject')
    : projectName(projectPath) || 'jcode'

  if (readOnly) {
    return (
      <div className="chat-panel flex min-h-0 flex-1 flex-col">
        <div className="min-h-0 flex-1">
          <Thread overscanBottom={8} renderPending={() => <PendingIndicator />} {...turnStrings} />
        </div>
      </div>
    )
  }

  if (conversationLoadPhase !== 'idle') return <ConversationLoadingView />

  // Resume in flight: swap to a skeleton the instant the click lands, so the
  // switch feels immediate instead of blank-until-ready (the old flow showed
  // nothing until the full history had fetched AND rebuilt).
  if (sessionLoading) {
    const label = t('chat.loadingConversation')
    return (
      <div className="chat-panel flex min-h-0 flex-1 flex-col">
        <div className="resume-skeleton jcode-chat-col" role="status" aria-live="polite" aria-label={label}>
          <div className="resume-skeleton__rows jcode-gutter">
            <div className="rs-row rs-row--user"><div className="rs-bar" style={{ width: '46%' }} /></div>
            <div className="rs-row"><div className="rs-bar" style={{ width: '78%' }} /></div>
            <div className="rs-row"><div className="rs-bar" style={{ width: '62%' }} /></div>
            <div className="rs-row rs-row--user"><div className="rs-bar" style={{ width: '38%' }} /></div>
            <div className="rs-row"><div className="rs-bar" style={{ width: '70%' }} /></div>
          </div>
          <span className="resume-skeleton__label">{label}</span>
        </div>
      </div>
    )
  }

  // Welcome screen: centered hero + elevated composer (no messages yet).
  if (!hasMessages) {
    const subtitle = t('welcome.subtitle')
    const title = workspaceKind === 'scratch'
      ? t('welcome.startWithoutProject')
      : t('welcome.startIn').replace('{project}', project)
    const [subtitleBefore, subtitleAfter] = subtitle.split('{kbd}')

    return (
      <div className="chat-panel welcome flex flex-1 flex-col items-center overflow-y-auto px-6">
        <ModelBackdrop kind={backdropKind} />
        <div className="welcome-aura" aria-hidden="true" />
        {/* Top half: hero floats above the centered composer. */}
        <div className="welcome-hero flex min-h-0 flex-1 flex-col items-center justify-end pb-10">
          <div className="welcome-logo select-none">
            <span className="wl-dim">[</span>
            <span className="wl-j">J</span>
            <span className="wl-fg">CODE</span>
            <span className="wl-dim">]</span>
          </div>
          <h2 className="welcome-title">{title}</h2>
          <p className="welcome-sub">
            {subtitleBefore}
            <kbd className="welcome-kbd">/</kbd>
            {subtitleAfter}
          </p>
        </div>
        {/* Centered elevated composer. z-[2] keeps its upward-opening menus
            (model picker, slash palette) above the welcome hero text. */}
        <div className="welcome-composer z-[2] w-full max-w-4xl px-5">
          <RemoteConnectionNotice />
          <ChatInput host={host} elevated pickerPlacement="bottom" onSent={() => { /* timeline auto-follows */ }} />
        </div>
        {/* Bottom half balances the center */}
        <div className="min-h-0 flex-1" aria-hidden="true" />
      </div>
    )
  }

  // Active conversation: feathered timeline + docked composer (no border-t).
  // `.chat-col` is shared with messages so the input box width matches the
  // message column exactly (same max-width + 20px horizontal inset).
  return (
    <div className="chat-panel flex min-h-0 flex-1 flex-col">
      <ModelBackdrop kind={backdropKind} />
      <div className="chat-content-layer min-h-0 flex-1">
        <Thread
          overscanBottom={28}
          hidePendingAskUser
          renderPending={pendingAskUser ? () => null : () => <PendingIndicator />}
          {...turnStrings}
        />
      </div>
      {/* z-[2] keeps the composer’s upward-opening menus above the thread layer. */}
      <div className="chat-content-layer chat-col relative z-[2]">
        <RemoteConnectionNotice />
        {/* Goal pill floats behind the composer; composer sits on top (higher z-index). */}
        <GoalBanner host={host} />
        <div className={pendingAskUser ? 'hidden' : undefined} aria-hidden={pendingAskUser ? true : undefined}>
          <ChatInput host={host} onSent={() => { /* timeline auto-follows via useStreamFollow */ }} />
        </div>
        {pendingAskUser && (
          <AskUserCard
            key={pendingAskUser.askUserId}
            tool={pendingAskUser}
            placement="dock"
            strings={{
              title: t('askUser.needsAnswer'),
              helper: t('askUser.helper'),
              previous: t('askUser.previous'),
              next: t('askUser.next'),
              skip: t('askUser.skip'),
              submit: t('askUser.submit'),
              submitting: t('askUser.submitting'),
              customPlaceholder: t('askUser.customPlaceholder'),
              recommended: t('askUser.recommended'),
              multiSelect: t('askUser.multiSelect'),
              skipped: t('askUser.skipped'),
              noAnswer: t('askUser.noAnswer'),
              submitError: t('askUser.submitError'),
            }}
          />
        )}
      </div>
    </div>
  )
}

function ModelBackdrop({ kind }: { kind: ModelBackdropKind | null }) {
  return (
    <div
      className={`model-backdrop${kind ? ' is-visible' : ''}`}
      data-model-backdrop={kind ?? undefined}
      aria-hidden="true"
    >
      {kind && <img key={kind} className="model-backdrop-asset" src={MODEL_BACKGROUNDS[kind]} alt="" />}
    </div>
  )
}

function modelBackdropKind(values: Array<string | undefined>): ModelBackdropKind | null {
  const identity = values.filter(Boolean).join('|').toLowerCase()
  if (/kimi|moonshot/.test(identity)) return 'kimi'
  if (/zhipu|bigmodel|(^|[|/ _-])zai(?=$|[|/ _-])|(^|[|/ _-])glm(?=$|[|/ _-])/.test(identity)) {
    return 'zhipu'
  }
  return null
}

function projectName(path: string): string {
  if (!path) return ''
  if (path.startsWith('ssh://') || path.startsWith('docker://')) {
    const clean = path.replace(/^ssh:\/\//, '').replace(/^docker:\/\//, '')
    const parts = clean.split('/').filter(Boolean)
    return parts[parts.length - 1] || clean
  }
  const parts = path.split('/').filter(Boolean)
  return parts[parts.length - 1] || path
}
