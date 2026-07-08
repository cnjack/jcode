/**
 * Composer — the headless message composer.
 *
 * Owns: textarea state, autosize, IME-safe key handling, send/queue/stop
 * dispatch, and a slash-command palette skeleton. Does NOT own styling or the
 * model/mode/workspace pickers (those are app-specific — the styled jcode-ui
 * `ChatInput` composes this primitive and layers them on).
 *
 * Streaming interaction: when the runtime reports `isRunning`, the send button
 * becomes a stop button, and `send()` routes to `enqueueMessage` instead of
 * `sendMessage` (type-ahead). The runtime drains the queue on each turn end.
 */

import { useCallback, useLayoutEffect, useRef, useState } from 'react'
import type { KeyboardEvent, ReactNode } from 'react'
import { useRuntimeActions, useRuntimeState } from '../runtime/context.js'
import type { ChatImage } from '../types/index.js'

export interface SlashCommand {
  /** The literal text inserted when chosen (e.g. '/goal'). */
  slash: string
  description?: string
}

export interface ComposerRenderSlots {
  /** Render the slash-command dropdown when `slashState` is open. */
  renderSlashMenu?: (state: SlashMenuState) => ReactNode
  /** Render queued-message chips above the textarea. */
  renderQueue?: (queued: { id: string; text: string; images?: ChatImage[] }[]) => ReactNode
  /** Render the send/stop button. `mode` is 'send' or 'stop'. */
  renderSubmitButton?: (mode: 'send' | 'stop', disabled: boolean) => ReactNode
  /** Render attached-image thumbnails below the textarea. */
  renderAttachments?: (imgs: ChatImage[], remove: (i: number) => void) => ReactNode
  /** Optional content rendered before the textarea inside the input row
   *  (e.g. a "+" menu button). */
  renderPrefix?: () => ReactNode
  /** Optional content rendered after the textarea (e.g. a context ring). */
  renderSuffix?: () => ReactNode
}

export interface ComposerProps extends ComposerRenderSlots {
  /** Placeholder text. */
  placeholder?: string
  /** Max textarea height in px before it scrolls internally. */
  maxRows?: number
  /** Slash commands (fetched by the host). Empty/undefined disables the menu. */
  slashCommands?: SlashCommand[]
  /** Whether image attachments are allowed (gated by model vision support). */
  allowImages?: boolean
  /** Max image size in bytes (default 10MB). */
  maxImageBytes?: number
  /** aria-label for the textarea. */
  ariaLabel?: string
  /** Controlled initial value (uncontrolled thereafter). */
  defaultValue?: string
  /** className passthrough. */
  className?: string
  /** Callback after a message is sent or queued (host snaps timeline to bottom). */
  onSent?: () => void
}

export interface SlashMenuState {
  open: boolean
  /** Filtered commands for the current input. */
  commands: SlashCommand[]
  /** Active (highlighted) index, or -1. */
  activeIndex: number
  /** Apply a command: inserts its slash text at the caret. */
  apply: (cmd: SlashCommand) => void
}

const DEFAULT_MAX_ROWS_PX = 160

export function Composer({
  placeholder = 'Send a message…',
  maxRows = DEFAULT_MAX_ROWS_PX,
  slashCommands,
  allowImages = false,
  maxImageBytes = 10 * 1024 * 1024,
  ariaLabel = 'Message input',
  defaultValue = '',
  className,
  onSent,
  renderSlashMenu,
  renderQueue,
  renderSubmitButton,
  renderAttachments,
  renderPrefix,
  renderSuffix,
}: ComposerProps): ReactNode {
  const actions = useRuntimeActions()
  const { isRunning, queued } = useRuntimeState()
  const [text, setText] = useState(defaultValue)
  const [images, setImages] = useState<ChatImage[]>([])
  const textareaRef = useRef<HTMLTextAreaElement>(null)

  // --- Autosize: grow with content up to maxRows, then scroll. ---
  useLayoutEffect(() => {
    const el = textareaRef.current
    if (!el) return
    el.style.height = 'auto'
    el.style.height = `${Math.min(el.scrollHeight, maxRows)}px`
  }, [text, maxRows])

  // --- Slash command menu: open when text starts with '/' and matches. ---
  const slashOpen = slashCommands && slashCommands.length > 0 && text.startsWith('/') && !text.includes(' ')
  const slashQuery = slashOpen ? text.slice(1).toLowerCase() : ''
  const filteredCommands = (slashCommands ?? []).filter((c) => c.slash.slice(1).toLowerCase().startsWith(slashQuery))
  const [slashActive, setSlashActive] = useState(0)
  // Reset active index when the filter changes.
  const filterKey = filteredCommands.map((c) => c.slash).join('|')
  const lastFilterKey = useRef(filterKey)
  if (lastFilterKey.current !== filterKey) {
    lastFilterKey.current = filterKey
    if (slashActive >= filteredCommands.length) setSlashActive(0)
  }

  const applySlash = useCallback((cmd: SlashCommand) => {
    setText(cmd.slash + ' ')
    textareaRef.current?.focus()
  }, [])

  // --- Send / queue / stop. ---
  const canSend = text.trim().length > 0 || images.length > 0
  const send = useCallback(() => {
    if (!canSend) return
    const imgs = images.length > 0 ? images : undefined
    if (isRunning) {
      actions.enqueueMessage(text.trim(), imgs)
    } else {
      actions.sendMessage(text.trim(), imgs)
    }
    setText('')
    setImages([])
    onSent?.()
  }, [actions, canSend, images, isRunning, onSent, text])

  const stop = useCallback(() => actions.stop(), [actions])

  // --- Key handling: Enter=send, Shift+Enter=newline, IME-safe, slash nav. ---
  const onKeyDown = useCallback(
    (e: KeyboardEvent<HTMLTextAreaElement>) => {
      // IME composition: never hijack.
      if (e.nativeEvent.isComposing || e.keyCode === 229) return

      if (slashOpen && filteredCommands.length > 0) {
        if (e.key === 'ArrowDown') {
          e.preventDefault()
          setSlashActive((i) => (i + 1) % filteredCommands.length)
          return
        }
        if (e.key === 'ArrowUp') {
          e.preventDefault()
          setSlashActive((i) => (i - 1 + filteredCommands.length) % filteredCommands.length)
          return
        }
        if (e.key === 'Enter' || e.key === 'Tab') {
          e.preventDefault()
          applySlash(filteredCommands[slashActive])
          return
        }
        if (e.key === 'Escape') {
          e.preventDefault()
          setText('')
          return
        }
      }

      if (e.key === 'Enter' && !e.shiftKey) {
        e.preventDefault()
        send()
      }
    },
    [applySlash, filteredCommands, send, slashActive, slashOpen],
  )

  // --- Image attachment: paste + remove. File picking is left to the host
  //     (it needs a file input + app-specific UX); addImage accepts a ready
  //     ChatImage. ---
  const addImage = useCallback(
    (img: ChatImage) => {
      if (!allowImages) return
      // Reject oversize by raw base64 length (~4/3 of bytes).
      const approxBytes = (img.data.length * 3) / 4
      if (approxBytes > maxImageBytes) return
      setImages((prev) => [...prev, img])
    },
    [allowImages, maxImageBytes],
  )
  const removeImage = useCallback((i: number) => {
    setImages((prev) => prev.filter((_, idx) => idx !== i))
  }, [])

  const mode: 'send' | 'stop' = isRunning ? 'stop' : 'send'

  return (
    <div className={className}>
      {renderQueue?.(queued)}
      {renderSlashMenu?.({
        open: !!slashOpen && filteredCommands.length > 0,
        commands: filteredCommands,
        activeIndex: slashOpen ? slashActive : -1,
        apply: applySlash,
      })}
      <div style={{ display: 'flex', alignItems: 'flex-end', gap: 8 }}>
        {renderPrefix?.()}
        <textarea
          ref={textareaRef}
          value={text}
          onChange={(e) => setText(e.target.value)}
          onKeyDown={onKeyDown}
          onPaste={(e) => {
            if (!allowImages) return
            const items = e.clipboardData?.items
            if (!items) return
            for (const it of items) {
              if (it.kind === 'file' && it.type.startsWith('image/')) {
                const file = it.getAsFile()
                if (!file) continue
                e.preventDefault()
                const reader = new FileReader()
                reader.onload = () => {
                  const dataUrl = String(reader.result)
                  const comma = dataUrl.indexOf(',')
                  addImage({ data: dataUrl.slice(comma + 1), media_type: file.type })
                }
                reader.readAsDataURL(file)
              }
            }
          }}
          placeholder={placeholder}
          aria-label={ariaLabel}
          rows={1}
          style={{ resize: 'none', flex: 1, overflowY: 'auto' }}
        />
        {renderSuffix?.()}
        {renderSubmitButton
          ? renderSubmitButton(mode, mode === 'send' ? !canSend : false)
          : (
            <button
              type="button"
              onClick={mode === 'send' ? send : stop}
              disabled={mode === 'send' ? !canSend : false}
              aria-label={mode === 'send' ? 'Send message' : 'Stop'}
            >
              {mode === 'send' ? 'Send' : 'Stop'}
            </button>
          )}
      </div>
      {allowImages && images.length > 0 && renderAttachments?.(images, removeImage)}
    </div>
  )
}
