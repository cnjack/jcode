/**
 * Message — styled chat bubble (wraps the headless MessageView).
 *
 * Adds: token-driven role styling (user right-aligned primary, assistant left,
 * system muted/error), markdown rendering via the marked+hljs+DOMPurify pipeline,
 * Geist font, and the copy/edit affordances styled with heroicons.
 */

import { memo, useMemo } from 'react'
import { CheckIcon, PencilSquareIcon, Square2StackIcon } from '@heroicons/react/24/outline'
import type { Message as MessageData } from 'jcode-ui-core'
import { MessageView } from 'jcode-ui-core/primitives'
import type { MessageActions } from 'jcode-ui-core/primitives'
import { renderMarkdown } from '../lib/markdown.js'
import { Reasoning } from './Reasoning.js'
import { Sources } from './Sources.js'

export interface MessageProps {
  message: MessageData
  /** Allow editing (typically user messages when idle). */
  canEdit?: boolean
}

export const Message = memo(function Message({ message, canEdit }: MessageProps) {
  const isUser = message.role === 'user'
  const isSystem = message.role === 'system'

  // System messages have their own muted/error treatment and no avatar.
  if (isSystem) {
    return <SystemMessage message={message} />
  }

  return (
    <div
      className={`jcode-message flex gap-3 px-4 py-2 ${isUser ? 'flex-row-reverse' : 'flex-row'}`}
      data-role={message.role}
      data-source={message.source}
    >
      <Avatar role={message.role} source={message.source} />
      <div className={`min-w-0 max-w-[85%] ${isUser ? 'items-end' : 'items-start'} flex flex-col`}>
        {message.reasoning && <Reasoning reasoning={message.reasoning} durationMs={message.durationMs} />}
        <Bubble message={message} canEdit={canEdit} isUser={isUser} />
        {message.sources && message.sources.length > 0 && <Sources sources={message.sources} />}
      </div>
    </div>
  )
})

function Bubble({ message, canEdit, isUser }: { message: MessageData; canEdit?: boolean; isUser: boolean }) {
  return (
    <div className="group/msg">
      <MessageView
        message={message}
        canEdit={canEdit}
        className={`jcode-selectable relative rounded-[var(--radius-lg)] px-3.5 py-2.5 text-[0.9rem] leading-relaxed ${
          isUser
            ? 'bg-[var(--color-primary)] text-[var(--color-on-primary)]'
            : 'bg-[var(--color-surface)] text-[var(--color-foreground)] border border-[var(--color-border)]'
        }`}
        renderContent={(content) => <MarkdownBody html={content} />}
        renderAvatar={() => null}
        renderActions={(actions) => <MessageActionsRow {...actions} />}
      />
    </div>
  )
}

/**
 * Icon-based hover actions (mirrors ChatMessage.vue lines 172-189): a copy
 * button (Square2StackIcon → CheckIcon when copied) and, for editable user
 * messages, an edit button (PencilSquareIcon). Both render inside a row that
 * fades in on group hover/focus.
 */
function MessageActionsRow({ copied, onCopy, canEdit, onEdit }: MessageActions) {
  return (
    <div className="flex items-center gap-0.5 pl-9 opacity-0 transition-opacity duration-150 group-hover/msg:opacity-100 group-focus-within/msg:opacity-100">
      <button
        type="button"
        onClick={onCopy}
        title={copied ? 'Copied' : 'Copy'}
        aria-label={copied ? 'Copied' : 'Copy'}
        className="flex h-5 w-5 items-center justify-center rounded-[var(--radius-sm)] cursor-pointer transition-all hover:bg-[var(--color-secondary)] active:scale-90"
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
          onClick={onEdit}
          title="Edit"
          aria-label="Edit"
          className="flex h-5 w-5 items-center justify-center rounded-[var(--radius-sm)] cursor-pointer transition-all hover:bg-[var(--color-secondary)] active:scale-90"
          style={{ color: 'var(--color-muted-foreground)' }}
        >
          <PencilSquareIcon className="h-3 w-3" />
        </button>
      )}
    </div>
  )
}

const MarkdownBody = memo(function MarkdownBody({ html }: { html: string }) {
  const sanitized = useMemo(() => renderMarkdown(html), [html])
  return (
    <div
      className="jcode-prose max-w-none break-words [&_p]:my-1 [&_ul]:my-1 [&_ol]:my-1 [&_li]:my-0"
      dangerouslySetInnerHTML={{ __html: sanitized }}
    />
  )
})

function Avatar({ role, source }: { role: MessageData['role']; source?: string }) {
  const glyph = role === 'user' ? 'U' : 'J'
  const tint = source === 'wechat' ? 'bg-[var(--color-success)] text-white' : 'bg-[var(--color-secondary)] text-[var(--color-secondary-foreground)]'
  return (
    <span
      className={`mt-0.5 inline-flex h-6 w-6 shrink-0 items-center justify-center rounded-full text-[0.7rem] font-semibold ${tint}`}
      aria-hidden
    >
      {glyph}
    </span>
  )
}

function SystemMessage({ message }: { message: MessageData }) {
  const isError = message.level === 'error'
  return (
    <div className="jcode-message flex justify-center px-4 py-1.5" data-role="system" data-level={message.level}>
      <div
        className={`max-w-[90%] rounded-[var(--radius-md)] px-3 py-1.5 text-center text-xs ${
          isError
            ? 'bg-[var(--color-error-bg)] text-[var(--color-error-fg)]'
            : 'bg-[var(--color-muted)] text-[var(--color-muted-foreground)]'
        }`}
      >
        {message.content}
        {message.detail && (
          <details className="mt-1 text-left">
            <summary className="cursor-pointer opacity-70">details</summary>
            <pre className="mt-1 whitespace-pre-wrap text-left font-mono text-[0.7rem] opacity-80">{message.detail}</pre>
          </details>
        )}
      </div>
    </div>
  )
}
