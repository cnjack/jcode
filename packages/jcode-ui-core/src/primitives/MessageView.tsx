/**
 * MessageView — the headless chat bubble.
 *
 * Owns: role-aware structure (avatar + label + body + actions), copy/edit
 * interactions, and image toggling. Does NOT own markdown rendering (passed in
 * via `renderContent`) or styling — the styled jcode-ui `Message` supplies the
 * marked+highlight.js+DOMPurify pipeline and token-driven classes.
 *
 * Streaming is invisible here: `MessageView` just re-renders when
 * `message.content` changes. The runtime owns accumulation.
 */

import { useCallback, useState } from 'react'
import type { ReactNode } from 'react'
import type { Message } from '../types/index.js'
import { useRuntimeActions } from '../runtime/context.js'

export interface MessageViewRenderSlots {
  /** Render the message body (markdown → sanitized HTML). Default: raw text. */
  renderContent?: (htmlOrText: string, message: Message) => ReactNode
  /** Render the avatar glyph for a role. */
  renderAvatar?: (role: Message['role']) => ReactNode
}

export interface MessageViewProps extends MessageViewRenderSlots {
  message: Message
  /** Whether the user may edit (typically role==='user' && !isRunning). */
  canEdit?: boolean
  /** Whether to show the copy action on hover. Default true. */
  showCopy?: boolean
  /** className passthrough. */
  className?: string
}

export function MessageView({
  message,
  canEdit = false,
  showCopy = true,
  className,
  renderContent,
  renderAvatar,
}: MessageViewProps): ReactNode {
  const actions = useRuntimeActions()
  const [editing, setEditing] = useState(false)
  const [draft, setDraft] = useState(message.content)
  const [copied, setCopied] = useState(false)

  const startEdit = useCallback(() => {
    setDraft(message.content)
    setEditing(true)
  }, [message.content])

  const saveEdit = useCallback(() => {
    setEditing(false)
    if (draft.trim() && draft !== message.content) {
      actions.editMessage(message.id, draft.trim())
    }
  }, [actions, draft, message.content, message.id])

  const cancelEdit = useCallback(() => setEditing(false), [])

  const copy = useCallback(async () => {
    try {
      await navigator.clipboard.writeText(message.content)
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    } catch {
      // clipboard unavailable
    }
  }, [message.content])

  return (
    <div className={className} data-role={message.role} data-message-level={message.level ?? undefined}>
      <div style={{ display: 'flex', gap: 8 }}>
        {renderAvatar ? renderAvatar(message.role) : <DefaultAvatar role={message.role} />}
        <div style={{ flex: 1 }}>
          {message.durationMs != null && message.role === 'assistant' && (
            <div style={{ opacity: 0.6 }}>{(message.durationMs / 1000).toFixed(1)}s</div>
          )}
          {editing ? (
            <div>
              <textarea
                value={draft}
                onChange={(e) => setDraft(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter' && !e.shiftKey) {
                    e.preventDefault()
                    saveEdit()
                  } else if (e.key === 'Escape') {
                    cancelEdit()
                  }
                }}
                style={{ width: '100%', minHeight: 80 }}
              />
              <div>
                <button type="button" onClick={saveEdit}>Save & resend</button>
                <button type="button" onClick={cancelEdit}>Cancel</button>
              </div>
            </div>
          ) : (
            <div>
              {renderContent ? renderContent(message.content, message) : message.content}
            </div>
          )}
          {message.images && message.images.length > 0 && (
            <div style={{ display: 'flex', gap: 8, marginTop: 4 }}>
              {message.images.map((img, i) => (
                <img
                  key={i}
                  src={`data:${img.media_type};base64,${img.data}`}
                  alt={`attachment ${i + 1}`}
                  style={{ maxWidth: 200, borderRadius: 4 }}
                />
              ))}
            </div>
          )}
          {showCopy && !editing && (
            <div style={{ opacity: 0.6 }}>
              <button type="button" onClick={copy}>{copied ? 'Copied' : 'Copy'}</button>
              {canEdit && <button type="button" onClick={startEdit}>Edit</button>}
            </div>
          )}
        </div>
      </div>
    </div>
  )
}

function DefaultAvatar({ role }: { role: Message['role'] }): ReactNode {
  const glyph = role === 'user' ? 'U' : role === 'assistant' ? 'J' : 'S'
  return <span aria-hidden style={{ width: 24, height: 24, display: 'inline-flex', alignItems: 'center', justifyContent: 'center', borderRadius: '50%', border: '1px solid' }}>{glyph}</span>
}
