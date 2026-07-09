/**
 * Message — flat chat message (matches web/src/components/ChatMessage.vue).
 *
 * Layout (NOT chat-bubble cards):
 *   [avatar] Role label
 *            markdown content (pl-9, no bg / no border)
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
      className="jcode-message chat-col group/msg animate-fade-in py-3"
      data-role={message.role}
      data-source={message.source}
      data-level={message.level}
    >
      {/* Role label + avatar — always left-aligned, no bubble chrome. */}
      <div className="mb-2 flex items-center gap-2.5">
        <div
          className="flex h-7 w-7 shrink-0 items-center justify-center rounded-full text-[10px] font-bold"
          style={{ background: avatarBg, color: 'var(--color-surface)' }}
          aria-hidden
        >
          {avatarGlyph}
        </div>
        <span className="text-[11px] font-semibold" style={{ color: labelColor }}>
          {roleLabel}
        </span>
      </div>

      {/* Attachments */}
      {message.images && message.images.length > 0 && (
        <div className="mb-2 flex flex-wrap gap-2 pl-9">
          {message.images.map((img, i) => (
            <img
              key={i}
              src={`data:${img.media_type};base64,${img.data}`}
              alt={`attachment ${i + 1}`}
              className="max-h-48 max-w-64 cursor-pointer object-contain transition-opacity hover:opacity-90"
              style={{
                borderRadius: 'var(--radius-lg)',
                border: '1px solid var(--color-border)',
              }}
            />
          ))}
        </div>
      )}

      {message.reasoning && (
        <div className="pl-9">
          <Reasoning reasoning={message.reasoning} durationMs={message.durationMs} />
        </div>
      )}

      {/* Body or inline edit — flat prose, no card bg/border. */}
      {editing ? (
        <div className="pl-9">
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
            className="min-h-20 max-h-80 w-full resize-y px-3 py-2 text-sm transition-colors"
            style={{
              borderRadius: 'var(--radius-lg)',
              border: '1px solid var(--color-border)',
              background: 'var(--color-surface)',
              color: 'var(--color-foreground)',
            }}
            // eslint-disable-next-line jsx-a11y/no-autofocus
            autoFocus
          />
          <div className="mt-2 flex items-center gap-2">
            <button
              type="button"
              onClick={saveEdit}
              className="cursor-pointer px-3 py-1 text-xs font-semibold transition-all active:scale-95"
              style={{
                background: 'var(--color-accent-neutral)',
                color: 'var(--color-surface)',
                borderRadius: 'var(--radius-md)',
              }}
            >
              Save
            </button>
            <button
              type="button"
              onClick={cancelEdit}
              className="cursor-pointer px-3 py-1 text-xs font-medium transition-all active:scale-95"
              style={{
                background: 'var(--color-secondary)',
                color: 'var(--color-foreground)',
                borderRadius: 'var(--radius-md)',
              }}
            >
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
        <div className="pl-9">
          <Sources sources={message.sources} />
        </div>
      )}

      {/* Action footer: duration (assistant) + hover copy/edit. */}
      {!editing && (
        <div className="mt-1.5 flex items-center gap-2 pl-9">
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
          <div className="flex items-center gap-0.5 opacity-0 transition-opacity duration-150 group-hover/msg:opacity-100 group-focus-within/msg:opacity-100">
            <button
              type="button"
              onClick={copy}
              title={copied ? 'Copied' : 'Copy'}
              aria-label={copied ? 'Copied' : 'Copy'}
              className="flex h-5 w-5 cursor-pointer items-center justify-center rounded-[var(--radius-sm)] transition-all hover:bg-[var(--color-secondary)] active:scale-90"
              style={{ color: 'var(--color-muted-foreground)' }}
            >
              {copied ? (
                <CheckIcon className="h-3 w-3" style={{ color: 'var(--color-accent-neutral)' }} />
              ) : (
                <Square2StackIcon className="h-3 w-3" />
              )}
            </button>
            {canEdit && (
              <button
                type="button"
                onClick={startEdit}
                title="Edit"
                aria-label="Edit"
                className="flex h-5 w-5 cursor-pointer items-center justify-center rounded-[var(--radius-sm)] transition-all hover:bg-[var(--color-secondary)] active:scale-90"
                style={{ color: 'var(--color-muted-foreground)' }}
              >
                <PencilSquareIcon className="h-3 w-3" />
              </button>
            )}
          </div>
        </div>
      )}

      {/* System error detail */}
      {isSystem && message.level === 'error' && message.detail && (
        <details className="mt-1 pl-9">
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
      className="jcode-prose jcode-selectable max-w-none break-words pl-9"
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
