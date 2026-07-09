/**
 * Message — flat chat message (matches web/src/components/ChatMessage.vue).
 *
 * Layout (NOT chat-bubble cards):
 *   [avatar] Role label
 *            markdown content (jcode-gutter, no bg / no border)
 *            duration · copy / edit (hover)
 *
 * User and assistant share the same left-aligned structure; only the avatar
 * fill and label color differ. System messages keep the same skeleton with
 * a level-tinted avatar.
 */

import { memo, useCallback, useMemo, useState } from 'react'
import { CheckIcon, PencilSquareIcon, Square2StackIcon } from '@heroicons/react/24/outline'
import type { Message as MessageData } from 'jcode-ui-core'
import { useRuntimeActions } from 'jcode-ui-core/runtime'
import { renderMarkdown } from '../lib/markdown.js'
import { AttachmentList } from './Attachment.js'
import { Reasoning } from './Reasoning.js'
import { Sources } from './Sources.js'

export interface MessageProps {
  message: MessageData
  /** Allow editing (typically user messages when idle). */
  canEdit?: boolean
}

export const Message = memo(function Message({ message, canEdit }: MessageProps) {
  const actions = useRuntimeActions()
  const [copied, setCopied] = useState(false)
  const [editing, setEditing] = useState(false)
  const [draft, setDraft] = useState(message.content)

  const isUser = message.role === 'user'
  const isSystem = message.role === 'system'
  const isWechat = isUser && message.source === 'wechat'

  const systemColor =
    message.level === 'error'
      ? 'var(--color-destructive, var(--color-error-fg))'
      : message.level === 'notice'
        ? 'var(--color-muted-foreground)'
        : 'var(--color-warning-fg)'

  const avatarBg = isSystem
    ? systemColor
    : message.role === 'assistant'
      ? 'var(--color-accent-neutral)'
      : isWechat
        ? 'var(--color-info-fg)'
        : 'var(--color-foreground)'

  const labelColor = avatarBg
  const roleLabel = isSystem
    ? message.level === 'error'
      ? 'Error'
      : 'System'
    : isWechat
      ? 'WeChat'
      : isUser
        ? 'You'
        : 'JCODE'

  const avatarGlyph = isSystem
    ? 'S'
    : isWechat
      ? 'W'
      : isUser
        ? 'U'
        : 'J'

  const durationLabel = formatDuration(message.durationMs)

  const copy = useCallback(async () => {
    try {
      await navigator.clipboard.writeText(message.content)
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    } catch {
      // clipboard unavailable
    }
  }, [message.content])

  const startEdit = useCallback(() => {
    setDraft(message.content)
    setEditing(true)
  }, [message.content])

  const saveEdit = useCallback(() => {
    const text = draft.trim()
    setEditing(false)
    if (text && text !== message.content) {
      actions.editMessage(message.id, text)
    }
  }, [actions, draft, message.content, message.id])

  const cancelEdit = useCallback(() => setEditing(false), [])

  return (
    <div
      className="jcode-message jcode-chat-col group/msg animate-fade-in py-3"
      data-role={message.role}
      data-source={message.source}
      data-level={message.level}
    >
      {/* Role label + avatar — always left-aligned, no bubble chrome. */}
      <div className="mb-2 flex items-center gap-2.5">
        <div
          className="jcode-msg-avatar flex h-7 w-7 shrink-0 items-center justify-center rounded-full text-[10px] font-bold"
          style={{ background: avatarBg, color: 'var(--color-surface)' }}
          aria-hidden
        >
          {avatarGlyph}
        </div>
        <span className="text-[11px] font-semibold tracking-wide" style={{ color: labelColor }}>
          {roleLabel}
        </span>
      </div>

      {/* Attachments — same Attachment tiles as composer (assistant-ui UserMessageAttachments). */}
      {message.images && message.images.length > 0 && (
        <div className="jcode-gutter mb-2">
          <AttachmentList images={message.images} size={96} preview />
        </div>
      )}

      {message.reasoning && (
        <div className="jcode-gutter">
          <Reasoning reasoning={message.reasoning} durationMs={message.durationMs} />
        </div>
      )}

      {/* Body or inline edit — flat prose, no card bg/border. */}
      {editing ? (
        <div className="jcode-gutter">
          <textarea
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter' && !e.shiftKey) {
                e.preventDefault()
                saveEdit()
              } else if (e.key === 'Escape') {
                e.preventDefault()
                cancelEdit()
              }
            }}
            className="min-h-20 max-h-80 w-full resize-y px-3 py-2.5 text-sm shadow-[var(--shadow-sm)] transition-[border-color,box-shadow] outline-none focus:border-[var(--color-primary)] focus:shadow-[0_0_0_3px_var(--accent-wash)]"
            style={{
              borderRadius: 'var(--radius-xl)',
              border: '1px solid var(--color-border)',
              background: 'var(--color-surface)',
              color: 'var(--color-foreground)',
              fontFamily: 'var(--font-sans)',
            }}
            // eslint-disable-next-line jsx-a11y/no-autofocus
            autoFocus
          />
          <div className="mt-2 flex items-center gap-2">
            <button type="button" onClick={saveEdit} className="jcode-btn jcode-btn-primary">
              Save
            </button>
            <button type="button" onClick={cancelEdit} className="jcode-btn jcode-btn-secondary">
              Cancel
            </button>
            <span className="text-[10px]" style={{ color: 'var(--color-muted-foreground)' }}>
              Enter to save · Esc to cancel
            </span>
          </div>
        </div>
      ) : (
        <MarkdownBody html={message.content} />
      )}

      {message.sources && message.sources.length > 0 && (
        <div className="jcode-gutter">
          <Sources sources={message.sources} />
        </div>
      )}

      {/* Action footer: duration (assistant) + hover copy/edit. */}
      {!editing && (
        <div className="jcode-gutter mt-1.5 flex items-center gap-2">
          {durationLabel && message.role === 'assistant' && (
            <span
              className="text-[10px] tabular-nums"
              style={{
                fontFamily: 'var(--font-mono)',
                color: 'var(--color-muted-foreground)',
                opacity: 0.7,
              }}
              title="Time this turn took"
            >
              {durationLabel}
            </span>
          )}
          <div className="jcode-msg-actions flex items-center gap-0.5">
            <button
              type="button"
              onClick={copy}
              title={copied ? 'Copied' : 'Copy'}
              aria-label={copied ? 'Copied' : 'Copy'}
              className="jcode-msg-action-btn flex h-6 w-6 cursor-pointer items-center justify-center rounded-[var(--radius-md)]"
            >
              {copied ? (
                <CheckIcon className="h-3.5 w-3.5" style={{ color: 'var(--color-success)' }} />
              ) : (
                <Square2StackIcon className="h-3.5 w-3.5" />
              )}
            </button>
            {canEdit && (
              <button
                type="button"
                onClick={startEdit}
                title="Edit"
                aria-label="Edit"
                className="jcode-msg-action-btn flex h-6 w-6 cursor-pointer items-center justify-center rounded-[var(--radius-md)]"
              >
                <PencilSquareIcon className="h-3.5 w-3.5" />
              </button>
            )}
          </div>
        </div>
      )}

      {/* System error detail */}
      {isSystem && message.level === 'error' && message.detail && (
        <details className="jcode-gutter mt-1">
          <summary className="cursor-pointer text-[11px] text-[var(--color-muted-foreground)]">
            details
          </summary>
          <pre
            className="mt-1 overflow-x-auto whitespace-pre-wrap px-2 py-1.5 font-mono text-[11px]"
            style={{
              color: 'var(--color-muted-foreground)',
              background: 'var(--color-muted)',
              borderRadius: 'var(--radius-md)',
            }}
          >
            {message.detail}
          </pre>
        </details>
      )}
    </div>
  )
})

const MarkdownBody = memo(function MarkdownBody({ html }: { html: string }) {
  const sanitized = useMemo(() => renderMarkdown(html), [html])
  return (
    <div
      className="jcode-prose jcode-selectable jcode-gutter max-w-none break-words"
      dangerouslySetInnerHTML={{ __html: sanitized }}
    />
  )
})

function formatDuration(ms?: number): string {
  if (!ms || ms < 0) return ''
  const totalSec = Math.round(ms / 1000)
  if (totalSec < 60) return `${totalSec}s`
  const m = Math.floor(totalSec / 60)
  const s = totalSec % 60
  return s ? `${m}m ${s}s` : `${m}m`
}
