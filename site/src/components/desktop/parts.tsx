import type { CSSProperties, ReactNode } from 'react'

/* Presentational pieces of the desktop-app replica.
   Used by the live interactive replica AND the Remotion demo —
   so they take plain props and animate nothing by themselves. */

export function WindowChrome({
  children,
  style,
}: {
  children: ReactNode
  style?: CSSProperties
}) {
  return (
    <div className="dk-window" style={style}>
      <div className="dk-titlebar">
        <span className="dk-traffic red" />
        <span className="dk-traffic yellow" />
        <span className="dk-traffic green" />
      </div>
      <div className="dk-frame">{children}</div>
    </div>
  )
}

export const WORKSPACES = [
  { name: 'jtype', count: 5 },
  { name: 'jbrowser', count: 2 },
  { name: 'jcode', count: 5 },
]

export function Sidebar({
  active,
  onSelect,
}: {
  active: string
  onSelect?: (name: string) => void
}) {
  return (
    <aside className="dk-side">
      <div className="dk-side-item strong">
        <span className="dk-ic">＋</span> New task <span className="dk-kbd">⌘N</span>
      </div>
      <div className="dk-side-item">
        <span className="dk-ic">✦</span> Automations
      </div>
      <div className="dk-side-item">
        <span className="dk-ic">◉</span> Channels
      </div>
      <div className="dk-side-label">workspace</div>
      {WORKSPACES.map((w) => (
        <button
          key={w.name}
          className={`dk-ws${active === w.name ? ' active' : ''}`}
          onClick={() => onSelect?.(w.name)}
          tabIndex={onSelect ? 0 : -1}
        >
          <span className="dk-chevron">›</span>
          <span className="dk-folder">▤</span>
          <span className="dk-ws-name">{w.name}</span>
          <span className="dk-ws-count">{w.count}</span>
        </button>
      ))}
      <div className="dk-side-foot">
        <span>☾</span>
        <span>⚙</span>
      </div>
    </aside>
  )
}

export function JcodeWordmark({ size = 30 }: { size?: number }) {
  return (
    <div className="dk-wordmark" style={{ fontSize: size }}>
      <span className="br">[</span>
      <span className="j">J</span>
      <span>CODE</span>
      <span className="br">]</span>
    </div>
  )
}

export function Composer({
  workspace,
  value,
  placeholder = 'Ask JCODE or type / for commands',
  onChange,
  onSend,
  sendActive,
}: {
  workspace: string
  value: string
  placeholder?: string
  onChange?: (v: string) => void
  onSend?: () => void
  sendActive?: boolean
}) {
  return (
    <div className="dk-composer">
      <div className="dk-composer-ws">
        <span className="dk-folder">▤</span> {workspace} <span className="dk-caret-down">⌄</span>
      </div>
      <div className="dk-composer-box">
        <textarea
          rows={2}
          value={value}
          placeholder={placeholder}
          onChange={(e) => onChange?.(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter' && !e.shiftKey) {
              e.preventDefault()
              onSend?.()
            }
          }}
          readOnly={!onChange}
        />
        <div className="dk-composer-row">
          <span className="dk-mini">＋</span>
          <span className="dk-access">🛡 Full access ⌄</span>
          <span className="dk-spring" />
          <span className="dk-model">✳ GLM-5.2 ⌄</span>
          <span className="dk-effort">✦ max ⌄</span>
          <button
            className={`dk-send${sendActive ? ' hot' : ''}`}
            onClick={onSend}
            aria-label="Send"
          >
            ▷ Send
          </button>
        </div>
      </div>
    </div>
  )
}

export type ToolStatus = 'running' | 'done'

export function ToolChip({
  icon,
  label,
  detail,
  status,
}: {
  icon: string
  label: string
  detail: string
  status: ToolStatus
}) {
  return (
    <div className={`dk-tool ${status}`}>
      <span className="dk-tool-ic">{icon}</span>
      <span className="dk-tool-label">{label}</span>
      <span className="dk-tool-detail">{detail}</span>
      <span className="dk-tool-status">{status === 'running' ? '◌' : '✓'}</span>
    </div>
  )
}

export function DiffCard({ file, lines }: { file: string; lines: string[] }) {
  return (
    <div className="dk-diff">
      <div className="dk-diff-file">{file}</div>
      {lines.map((l, i) => (
        <div
          key={i}
          className={`dk-diff-line${l.startsWith('+') ? ' add' : l.startsWith('-') ? ' del' : ''}`}
        >
          {l}
        </div>
      ))}
    </div>
  )
}

export function UserBubble({ children }: { children: ReactNode }) {
  return <div className="dk-user-msg">{children}</div>
}

export function AgentText({ children }: { children: ReactNode }) {
  return (
    <div className="dk-agent-msg">
      <span className="dk-agent-diamond">◆</span>
      <div>{children}</div>
    </div>
  )
}

export function DoneBanner({ summary }: { summary: string }) {
  return (
    <div className="dk-done">
      <span className="dk-done-check">✓</span>
      <div>
        <b>Task completed</b>
        <span>{summary}</span>
      </div>
    </div>
  )
}
