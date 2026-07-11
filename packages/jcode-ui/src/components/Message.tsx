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

import { memo, useCallback, useRef, useState } from 'react'
import type { ReactNode } from 'react'
import {
  ArrowPathIcon,
  CheckIcon,
  HandThumbDownIcon,
  HandThumbUpIcon,
  PencilSquareIcon,
  Square2StackIcon,
} from '@heroicons/react/24/outline'
import type { Message as MessageData } from 'jcode-ui-core'
import { useRuntimeActions } from 'jcode-ui-core/runtime'
import { bindCodeBlockCopy } from '../lib/markdown.js'
import { useStreamingMarkdown } from '../lib/useStreamingMarkdown.js'
import { AttachmentList } from './Attachment.js'
import { BranchPicker } from './BranchPicker.js'
import { Reasoning } from './Reasoning.js'
import { Sources } from './Sources.js'

/** Render-prop overrides for the message chrome. Each replaces a piece of the
 *  default layout; omitting one keeps the built-in rendering unchanged. */
export interface MessageSlots {
  /** Replaces the avatar circle (inside the default header row). */
  avatar?: (message: MessageData) => ReactNode
  /** Replaces the entire role-header row (avatar + label). */
  header?: (message: MessageData) => ReactNode
  /** Appended to the tail of the action footer. */
  footerExtra?: (message: MessageData) => ReactNode
}

export interface MessageProps {
  message: MessageData
  /** Allow editing (typically user messages when idle). */
  canEdit?: boolean
  /** Optional chrome overrides (avatar / header / footer tail). */
  slots?: MessageSlots
}

export const Message = memo(function Message({ message, canEdit, slots }: MessageProps) {
  const actions = useRuntimeActions()
  const [copied, setCopied] = useState(false)
  const [editing, setEditing] = useState(false)
  const [draft, setDraft] = useState(message.content)

  const isUser = message.role === 'user'
  const isSystem = message.role === 'system'
  const isAssistant = message.role === 'assistant'
  const isWechat = isUser && message.source === 'wechat'

  const systemColor =
    message.level === 'error'
      ? 'var(--jcode-color-destructive, var(--jcode-color-error-fg))'
      : message.level === 'notice'
        ? 'var(--jcode-color-muted-foreground)'
        : 'var(--jcode-color-warning-fg)'

  const avatarBg = isSystem
    ? systemColor
    : message.role === 'assistant'
      ? 'var(--jcode-color-accent-neutral)'
      : isWechat
        ? 'var(--jcode-color-info-fg)'
        : 'var(--jcode-color-foreground)'

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

  const regenerate = useCallback(() => {
    actions.regenerate?.(message.id)
  }, [actions, message.id])

  const giveFeedback = useCallback(
    (rating: 'up' | 'down') => {
      // A recorded rating locks its own button; the opposite one re-rates.
      if (message.feedback === rating) return
      actions.submitFeedback?.(message.id, rating)
    },
    [actions, message.feedback, message.id],
  )

  const retry = useCallback(() => {
    actions.retryMessage?.(message.id)
  }, [actions, message.id])

  return (
    <div
      data-jcode-ui="" className="jcode-message jcode-chat-col group/msg jcode-animate-fade-in pt-3 pb-1.5"
      data-role={message.role}
      data-source={message.source}
      data-level={message.level}
    >
      {/* Role label + avatar — always left-aligned, no bubble chrome. */}
      {slots?.header ? (
        slots.header(message)
      ) : (
        <div className="mb-2 flex items-center gap-2.5">
          {slots?.avatar ? (
            slots.avatar(message)
          ) : (
            <div
              className="jcode-msg-avatar flex h-7 w-7 shrink-0 items-center justify-center rounded-full text-[10px] font-bold"
              style={{ background: avatarBg, color: 'var(--jcode-color-surface)' }}
              aria-hidden
            >
              {avatarGlyph}
            </div>
          )}
          <span className="text-[11px] font-semibold tracking-wide" style={{ color: labelColor }}>
            {roleLabel}
          </span>
        </div>
      )}

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
            className="min-h-20 max-h-80 w-full resize-y px-3 py-2.5 text-sm shadow-[var(--jcode-shadow-sm)] transition-[border-color,box-shadow] outline-none focus:border-[var(--jcode-color-primary)] focus:shadow-[0_0_0_3px_var(--jcode-accent-wash)]"
            style={{
              borderRadius: 'var(--jcode-radius-xl)',
              border: '1px solid var(--jcode-color-border)',
              background: 'var(--jcode-color-surface)',
              color: 'var(--jcode-color-foreground)',
              fontFamily: 'var(--jcode-font-sans)',
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
            <span className="text-[10px]" style={{ color: 'var(--jcode-color-muted-foreground)' }}>
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

      {/* Action footer: branch stepper · duration · copy/edit/regenerate/feedback. */}
      {!editing && (
        <div className="jcode-gutter mt-0.5 flex items-center gap-2">
          <BranchPicker message={message} />
          {durationLabel && isAssistant && (
            <span
              className="text-[10px] tabular-nums"
              style={{
                fontFamily: 'var(--jcode-font-mono)',
                color: 'var(--jcode-color-muted-foreground)',
                opacity: 0.7,
              }}
              title="Time this turn took"
            >
              {durationLabel}
            </span>
          )}
          <div
            className="jcode-msg-actions flex items-center gap-0.5"
            data-has-feedback={message.feedback ? 'true' : undefined}
          >
            <button
              type="button"
              onClick={copy}
              title={copied ? 'Copied' : 'Copy'}
              aria-label={copied ? 'Copied' : 'Copy'}
              className="jcode-msg-action-btn flex h-5 w-5 cursor-pointer items-center justify-center rounded-[var(--jcode-radius-md)]"
            >
              {copied ? (
                <CheckIcon className="h-3.5 w-3.5" style={{ color: 'var(--jcode-color-success)' }} />
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
                className="jcode-msg-action-btn flex h-5 w-5 cursor-pointer items-center justify-center rounded-[var(--jcode-radius-md)]"
              >
                <PencilSquareIcon className="h-3.5 w-3.5" />
              </button>
            )}
            {isAssistant && actions.regenerate && (
              <button
                type="button"
                onClick={regenerate}
                title="Regenerate"
                aria-label="Regenerate response"
                className="jcode-msg-action-btn flex h-5 w-5 cursor-pointer items-center justify-center rounded-[var(--jcode-radius-md)]"
              >
                <ArrowPathIcon className="h-3.5 w-3.5" />
              </button>
            )}
            {isAssistant && actions.submitFeedback && (
              <>
                <button
                  type="button"
                  onClick={() => giveFeedback('up')}
                  disabled={message.feedback === 'up'}
                  data-active={message.feedback === 'up' ? 'true' : undefined}
                  title="Good response"
                  aria-label="Good response"
                  aria-pressed={message.feedback === 'up'}
                  className="jcode-feedback-btn"
                >
                  <HandThumbUpIcon className="h-3.5 w-3.5" />
                </button>
                <button
                  type="button"
                  onClick={() => giveFeedback('down')}
                  disabled={message.feedback === 'down'}
                  data-active={message.feedback === 'down' ? 'true' : undefined}
                  title="Bad response"
                  aria-label="Bad response"
                  aria-pressed={message.feedback === 'down'}
                  className="jcode-feedback-btn"
                >
                  <HandThumbDownIcon className="h-3.5 w-3.5" />
                </button>
              </>
            )}
          </div>
          {slots?.footerExtra && slots.footerExtra(message)}
        </div>
      )}

      {/* System error detail */}
      {isSystem && message.level === 'error' && message.detail && (
        <details className="jcode-gutter mt-1">
          <summary className="cursor-pointer text-[11px] text-[var(--jcode-color-muted-foreground)]">
            details
          </summary>
          <pre
            className="mt-1 overflow-x-auto whitespace-pre-wrap px-2 py-1.5 font-mono text-[11px]"
            style={{
              color: 'var(--jcode-color-muted-foreground)',
              background: 'var(--jcode-color-muted)',
              borderRadius: 'var(--jcode-radius-md)',
            }}
          >
            {message.detail}
          </pre>
        </details>
      )}

      {/* Failed-turn retry — only when the host wired retryMessage. */}
      {isSystem && message.level === 'error' && actions.retryMessage && (
        <div className="jcode-gutter jcode-retry-row">
          <button
            type="button"
            onClick={retry}
            className="jcode-btn jcode-btn-secondary jcode-retry-btn"
          >
            <ArrowPathIcon className="h-3.5 w-3.5" />
            Retry
          </button>
        </div>
      )}
    </div>
  )
})

const MarkdownBody = memo(function MarkdownBody({ html }: { html: string }) {
  // Streaming-stable rendering: unclosed fences/emphasis are completed before
  // parse, and finished top-level blocks are cached so long threads don't
  // re-render whole documents per token.
  const sanitized = useStreamingMarkdown(html)
  const unbindRef = useRef<(() => void) | null>(null)
  const bind = useCallback((el: HTMLDivElement | null) => {
    unbindRef.current?.()
    unbindRef.current = el ? bindCodeBlockCopy(el) : null
  }, [])
  return (
    <div
      ref={bind}
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
