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

import { Thread } from 'jcode-ui'
import { useTranslation } from 'react-i18next'
import { useAppSelector } from '../app/hooks'
import { GoalBanner } from './GoalBanner'
import { ChatInput } from './ChatInput'
import kimiBackground from '../assets/kimi-light-background.webp'
import zhipuBackground from '../assets/zhipu-light-background.webp'

const MODEL_BACKGROUNDS = {
  kimi: kimiBackground,
  zhipu: zhipuBackground,
} as const

type ModelBackdropKind = keyof typeof MODEL_BACKGROUNDS

export interface ChatViewProps {
  /** Read-only mode (automation-run replay): no composer, no follow. */
  readOnly?: boolean
}

export function ChatView({ readOnly }: ChatViewProps) {
  const { t } = useTranslation()
  const hasMessages = useAppSelector((s) => s.chat.timeline.length > 0)
  const projectPath = useAppSelector((s) => s.session.projectPath)
  const backdropKind = useAppSelector((s) => {
    const provider = s.model.providers.find((candidate) => candidate.id === s.model.providerName)
    const model = provider?.models.find((candidate) => candidate.id === s.model.modelName)
    return modelBackdropKind([s.model.providerName, provider?.name, s.model.modelName, model?.name])
  })
  const project = projectName(projectPath) || 'jcode'

  if (readOnly) {
    return (
      <div className="chat-panel flex min-h-0 flex-1 flex-col">
        <div className="min-h-0 flex-1">
          <Thread overscanBottom={8} />
        </div>
      </div>
    )
  }

  // Welcome screen: centered hero + elevated composer (no messages yet).
  if (!hasMessages) {
    const subtitle = t('welcome.subtitle')
    const title = t('welcome.startIn').replace('{project}', project)
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
        <div className="welcome-composer z-[2] w-full max-w-2xl px-5">
          <ChatInput elevated pickerPlacement="bottom" onSent={() => { /* timeline auto-follows */ }} />
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
        <Thread overscanBottom={28} />
      </div>
      {/* z-[2] keeps the composer’s upward-opening menus above the thread layer. */}
      <div className="chat-content-layer chat-col relative z-[2]">
        {/* Goal pill floats behind the composer; composer sits on top (higher z-index). */}
        <GoalBanner />
        <ChatInput onSent={() => { /* timeline auto-follows via useStreamFollow */ }} />
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
