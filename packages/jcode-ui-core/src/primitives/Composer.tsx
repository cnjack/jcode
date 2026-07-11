/**
 * Composer — the headless message composer.
 *
 * Owns: textarea state, autosize, IME-safe key handling, send/queue/stop
 * dispatch, a slash-command palette skeleton, and (Composer 2) a pluggable
 * attachment pipeline, drag/paste ingestion, control slots, dictation, and an
 * imperative `ComposerHandle`. Does NOT own styling or the model/mode/workspace
 * pickers (those are app-specific — the styled jcode-ui `ChatInput` composes
 * this primitive and layers them on).
 *
 * Streaming interaction: when the runtime reports `isRunning`, the send button
 * becomes a stop button, and `send()` routes to `enqueueMessage` instead of
 * `sendMessage` (type-ahead). The runtime drains the queue on each turn end.
 *
 * Attachments: with no `attachmentAdapter` the legacy `allowImages` base64 path
 * is used (unchanged). With an adapter, every picked/pasted/dropped file flows
 * through `adapter.add`, tracked in a pending state machine (uploading →
 * done/error). On send, completed image attachments still ride the ChatImage
 * `images` argument (so `sendMessage` stays compatible) and the full completed
 * set is also handed to `onSendAttachments`.
 */

import {
  forwardRef,
  useCallback,
  useEffect,
  useImperativeHandle,
  useLayoutEffect,
  useRef,
  useState,
} from 'react'
import type { ClipboardEvent, DragEvent, KeyboardEvent, ReactNode } from 'react'
import { useRuntimeActions, useRuntimeState } from '../runtime/context.js'
import type { ChatImage } from '../types/index.js'
import { nextAttachmentId } from './attachmentAdapter.js'
import type { AttachmentAdapter, PendingAttachment } from './attachmentAdapter.js'

export interface SlashCommand {
  /** The literal text inserted when chosen (e.g. '/goal'). */
  slash: string
  description?: string
}

/** Lifecycle status of a pending attachment slot. */
export type PendingStatus = 'uploading' | 'done' | 'error'

/** A pending-attachment row surfaced to `renderPendingAttachments`. */
export interface PendingAttachmentItem {
  /** Latest adapter snapshot (or the provisional entry while uploading). */
  attachment: PendingAttachment
  status: PendingStatus
  /** Remove this attachment (also invokes `adapter.remove` when present). */
  remove: () => void
  /** Re-run `adapter.add` for this file (error recovery). */
  retry: () => void
}

/** Dictation (speech-to-text) UI state passed to `renderDictationButton`. */
export interface DictationState {
  listening: boolean
}

/** Imperative handle exposed via `ref` (used by quote / insert features). */
export interface ComposerHandle {
  /** Insert text at the caret (replacing any selection) and refocus. */
  insertText(text: string): void
  /** Focus the textarea. */
  focus(): void
}

export interface ComposerRenderSlots {
  /** Render the slash-command dropdown when `slashState` is open. */
  renderSlashMenu?: (state: SlashMenuState) => ReactNode
  /** Render queued-message chips above the textarea. */
  renderQueue?: (queued: { id: string; text: string; images?: ChatImage[] }[]) => ReactNode
  /** Render the send/stop button. `mode` is 'send' or 'stop'.
   *  Call `onActivate` on click (send when idle, stop when running). */
  renderSubmitButton?: (mode: 'send' | 'stop', disabled: boolean, onActivate: () => void) => ReactNode
  /** Render attached-image thumbnails (legacy `allowImages` strip). */
  renderAttachments?: (imgs: ChatImage[], remove: (i: number) => void) => ReactNode
  /** Render the pending-attachment strip (adapter path). */
  renderPendingAttachments?: (items: PendingAttachmentItem[]) => ReactNode
  /**
   * Render the "add attachment" control (paperclip / +). Called with
   * `openPicker` that opens the hidden file input. When omitted and
   * attachments are enabled, a minimal default button is rendered.
   * Mirrors assistant-ui `ComposerAddAttachment`.
   */
  renderAddAttachment?: (openPicker: () => void) => ReactNode
  /** Render an overlay while files are dragged over the composer. */
  renderDropOverlay?: () => ReactNode
  /** Render the dictation (microphone) button. Only called when supported. */
  renderDictationButton?: (state: DictationState, toggle: () => void) => ReactNode
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
  /** `accept` attribute for the file picker (default `image/*`). */
  acceptImages?: string
  /** Max image size in bytes (default 10MB). */
  maxImageBytes?: number
  /**
   * Pluggable attachment pipeline. When provided it supersedes the legacy
   * base64 image path: the picker/paste/drop route files through the adapter
   * and a pending state machine tracks upload progress.
   */
  attachmentAdapter?: AttachmentAdapter
  /** Fired on send with the completed attachments (adapter path). */
  onSendAttachments?: (attachments: PendingAttachment[]) => void
  /** Content rendered after the add-attachment control (e.g. a ModelSelector). */
  leadingControls?: ReactNode
  /** Content rendered just before the submit button. */
  trailingControls?: ReactNode
  /** Content rendered below the composer row. */
  footer?: ReactNode
  /** Enable the dictation button (rendered only when the browser supports it). */
  enableDictation?: boolean
  /** BCP-47 language tag for dictation (default: browser default). */
  dictationLang?: string
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

interface PendingSlot {
  /** Stable react/tracking key (also used as the canonical attachment id). */
  key: string
  file: File
  attachment: PendingAttachment
  status: PendingStatus
}

function readImageFile(file: File): Promise<ChatImage | null> {
  return new Promise((resolve) => {
    if (!file.type.startsWith('image/')) {
      resolve(null)
      return
    }
    const reader = new FileReader()
    reader.onload = () => {
      const dataUrl = String(reader.result ?? '')
      const comma = dataUrl.indexOf(',')
      if (comma < 0) {
        resolve(null)
        return
      }
      resolve({
        data: dataUrl.slice(comma + 1),
        media_type: file.type,
        name: file.name || undefined,
      })
    }
    reader.onerror = () => resolve(null)
    reader.readAsDataURL(file)
  })
}

// ─── Dictation (SpeechRecognition) — minimal ambient typing, SSR-safe ─────────

interface SpeechRecognitionResultLike {
  isFinal: boolean
  0: { transcript: string }
  length: number
}
interface SpeechRecognitionEventLike {
  resultIndex: number
  results: { length: number } & Record<number, SpeechRecognitionResultLike>
}
interface SpeechRecognitionLike {
  continuous: boolean
  interimResults: boolean
  lang: string
  start(): void
  stop(): void
  onresult: ((e: SpeechRecognitionEventLike) => void) | null
  onend: (() => void) | null
  onerror: (() => void) | null
}
type SpeechRecognitionCtor = new () => SpeechRecognitionLike

function getSpeechRecognitionCtor(): SpeechRecognitionCtor | null {
  if (typeof window === 'undefined') return null
  const w = window as unknown as {
    SpeechRecognition?: SpeechRecognitionCtor
    webkitSpeechRecognition?: SpeechRecognitionCtor
  }
  return w.SpeechRecognition ?? w.webkitSpeechRecognition ?? null
}

export const Composer = forwardRef<ComposerHandle, ComposerProps>(function Composer(
  {
    placeholder = 'Send a message…',
    maxRows = DEFAULT_MAX_ROWS_PX,
    slashCommands,
    allowImages = false,
    acceptImages = 'image/*',
    maxImageBytes = 10 * 1024 * 1024,
    attachmentAdapter,
    onSendAttachments,
    leadingControls,
    trailingControls,
    footer,
    enableDictation = false,
    dictationLang,
    ariaLabel = 'Message input',
    defaultValue = '',
    className,
    onSent,
    renderSlashMenu,
    renderQueue,
    renderSubmitButton,
    renderAttachments,
    renderPendingAttachments,
    renderAddAttachment,
    renderDropOverlay,
    renderDictationButton,
    renderPrefix,
    renderSuffix,
  }: ComposerProps,
  ref,
): ReactNode {
  const actions = useRuntimeActions()
  const { isRunning, queued } = useRuntimeState()
  const [text, setText] = useState(defaultValue)
  const [images, setImages] = useState<ChatImage[]>([])
  const [pending, setPending] = useState<PendingSlot[]>([])
  const [dropping, setDropping] = useState(false)
  const [dictating, setDictating] = useState(false)
  const textareaRef = useRef<HTMLTextAreaElement>(null)
  const fileInputRef = useRef<HTMLInputElement>(null)
  const dragDepth = useRef(0)

  const attachmentsEnabled = !!attachmentAdapter || allowImages

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

  // --- Imperative handle (quote / insert). ---
  const insertText = useCallback((insert: string) => {
    const el = textareaRef.current
    setText((prev) => {
      const start = el ? el.selectionStart : prev.length
      const end = el ? el.selectionEnd : prev.length
      const next = prev.slice(0, start) + insert + prev.slice(end)
      requestAnimationFrame(() => {
        if (!el) return
        el.focus()
        const pos = start + insert.length
        el.setSelectionRange(pos, pos)
      })
      return next
    })
  }, [])

  useImperativeHandle(
    ref,
    () => ({
      insertText,
      focus: () => textareaRef.current?.focus(),
    }),
    [insertText],
  )

  // --- Adapter attachment pipeline (pending state machine). ---
  const runAdapterAdd = useCallback(
    (key: string, file: File) => {
      if (!attachmentAdapter) return
      attachmentAdapter
        .add(file, (p) => {
          setPending((prev) =>
            prev.map((s) => (s.key === key ? { ...s, attachment: { ...s.attachment, progress: p } } : s)),
          )
        })
        .then((result) => {
          setPending((prev) =>
            prev.map((s) =>
              s.key === key
                ? {
                    ...s,
                    // Keep the stable key as the canonical id.
                    attachment: { ...result, id: key },
                    status: result.error ? 'error' : 'done',
                  }
                : s,
            ),
          )
        })
        .catch((err: unknown) => {
          const message = err instanceof Error ? err.message : String(err)
          setPending((prev) =>
            prev.map((s) =>
              s.key === key ? { ...s, attachment: { ...s.attachment, error: message }, status: 'error' } : s,
            ),
          )
        })
    },
    [attachmentAdapter],
  )

  const addFilesViaAdapter = useCallback(
    (files: FileList | File[]) => {
      if (!attachmentAdapter) return
      for (const file of Array.from(files)) {
        const key = nextAttachmentId()
        const provisional: PendingAttachment = {
          id: key,
          kind: file.type.startsWith('image/') ? 'image' : 'file',
          name: file.name || 'attachment',
          size: file.size,
          media_type: file.type || undefined,
          progress: 0,
        }
        setPending((prev) => [...prev, { key, file, attachment: provisional, status: 'uploading' }])
        runAdapterAdd(key, file)
      }
    },
    [attachmentAdapter, runAdapterAdd],
  )

  const removePending = useCallback(
    (key: string) => {
      setPending((prev) => prev.filter((s) => s.key !== key))
      void attachmentAdapter?.remove?.(key)
    },
    [attachmentAdapter],
  )

  const retryPending = useCallback(
    (key: string) => {
      setPending((prev) =>
        prev.map((s) =>
          s.key === key
            ? { ...s, status: 'uploading', attachment: { ...s.attachment, error: undefined, progress: 0 } }
            : s,
        ),
      )
      const slot = pending.find((s) => s.key === key)
      if (slot) runAdapterAdd(key, slot.file)
    },
    [pending, runAdapterAdd],
  )

  const pendingItems: PendingAttachmentItem[] = pending.map((s) => ({
    attachment: s.attachment,
    status: s.status,
    remove: () => removePending(s.key),
    retry: () => retryPending(s.key),
  }))

  // --- Legacy image attachments (no adapter). ---
  const addImage = useCallback(
    (img: ChatImage) => {
      if (!allowImages) return
      const approxBytes = (img.data.length * 3) / 4
      if (approxBytes > maxImageBytes) return
      setImages((prev) => [...prev, img])
    },
    [allowImages, maxImageBytes],
  )
  const addImageFiles = useCallback(
    async (files: FileList | File[]) => {
      if (!allowImages) return
      for (const file of Array.from(files)) {
        if (file.size > maxImageBytes) continue
        const img = await readImageFile(file)
        if (img) addImage(img)
      }
    },
    [addImage, allowImages, maxImageBytes],
  )
  const removeImage = useCallback((i: number) => {
    setImages((prev) => prev.filter((_, idx) => idx !== i))
  }, [])

  // --- Unified file ingestion (picker / paste / drop). ---
  const ingestFiles = useCallback(
    (files: FileList | File[]) => {
      if (attachmentAdapter) addFilesViaAdapter(files)
      else void addImageFiles(files)
    },
    [addFilesViaAdapter, addImageFiles, attachmentAdapter],
  )

  const openPicker = useCallback(() => {
    fileInputRef.current?.click()
  }, [])

  // --- Send / queue / stop. ---
  const doneAttachments = pending.filter((s) => s.status === 'done').map((s) => s.attachment)
  const canSend =
    text.trim().length > 0 || images.length > 0 || doneAttachments.length > 0
  const send = useCallback(() => {
    if (!canSend) return
    const doneNow = pending.filter((s) => s.status === 'done').map((s) => s.attachment)
    const attachmentImages: ChatImage[] = doneNow
      .filter((a) => a.kind === 'image' && a.data)
      .map((a) => ({ data: a.data as string, media_type: a.media_type || 'image/*', name: a.name }))
    const allImages = [...images, ...attachmentImages]
    const imgs = allImages.length > 0 ? allImages : undefined
    if (isRunning) {
      actions.enqueueMessage(text.trim(), imgs)
    } else {
      actions.sendMessage(text.trim(), imgs)
    }
    if (doneNow.length > 0) onSendAttachments?.(doneNow)
    setText('')
    setImages([])
    setPending([])
    onSent?.()
  }, [actions, canSend, images, isRunning, onSendAttachments, onSent, pending, text])

  const stop = useCallback(() => actions.stop(), [actions])

  // --- Dictation. ---
  const recognitionRef = useRef<SpeechRecognitionLike | null>(null)
  const dictBaseRef = useRef('')
  const dictFinalRef = useRef('')
  const dictationSupported = enableDictation && getSpeechRecognitionCtor() !== null

  const toggleDictation = useCallback(() => {
    const Ctor = getSpeechRecognitionCtor()
    if (!Ctor) return
    if (recognitionRef.current) {
      recognitionRef.current.stop()
      return
    }
    const rec = new Ctor()
    rec.continuous = true
    rec.interimResults = true
    if (dictationLang) rec.lang = dictationLang
    dictFinalRef.current = ''
    setText((prev) => {
      dictBaseRef.current = prev ? prev.replace(/\s*$/, '') + ' ' : ''
      return prev
    })
    rec.onresult = (e: SpeechRecognitionEventLike) => {
      let interim = ''
      for (let i = e.resultIndex; i < e.results.length; i++) {
        const r = e.results[i]
        if (!r) continue
        if (r.isFinal) dictFinalRef.current += r[0].transcript
        else interim += r[0].transcript
      }
      setText(dictBaseRef.current + dictFinalRef.current + interim)
    }
    rec.onend = () => {
      recognitionRef.current = null
      setDictating(false)
    }
    rec.onerror = () => {
      recognitionRef.current = null
      setDictating(false)
    }
    recognitionRef.current = rec
    setDictating(true)
    rec.start()
  }, [dictationLang])

  useEffect(
    () => () => {
      recognitionRef.current?.stop()
      recognitionRef.current = null
    },
    [],
  )

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

  const onPaste = useCallback(
    (e: ClipboardEvent<HTMLTextAreaElement>) => {
      if (!attachmentsEnabled) return
      const items = e.clipboardData?.items
      if (!items) return
      const files: File[] = []
      for (const it of items) {
        // Adapter accepts any file kind; legacy path only images.
        const wanted = attachmentAdapter ? it.kind === 'file' : it.kind === 'file' && it.type.startsWith('image/')
        if (wanted) {
          const file = it.getAsFile()
          if (file) files.push(file)
        }
      }
      if (files.length === 0) return
      e.preventDefault()
      ingestFiles(files)
    },
    [attachmentAdapter, attachmentsEnabled, ingestFiles],
  )

  // --- Drag & drop. ---
  const onDragOver = useCallback(
    (e: DragEvent<HTMLDivElement>) => {
      if (!attachmentsEnabled) return
      if (!Array.from(e.dataTransfer?.types ?? []).includes('Files')) return
      e.preventDefault()
    },
    [attachmentsEnabled],
  )
  const onDragEnter = useCallback(
    (e: DragEvent<HTMLDivElement>) => {
      if (!attachmentsEnabled) return
      if (!Array.from(e.dataTransfer?.types ?? []).includes('Files')) return
      e.preventDefault()
      dragDepth.current += 1
      setDropping(true)
    },
    [attachmentsEnabled],
  )
  const onDragLeave = useCallback(
    (e: DragEvent<HTMLDivElement>) => {
      if (!attachmentsEnabled) return
      e.preventDefault()
      dragDepth.current = Math.max(0, dragDepth.current - 1)
      if (dragDepth.current === 0) setDropping(false)
    },
    [attachmentsEnabled],
  )
  const onDrop = useCallback(
    (e: DragEvent<HTMLDivElement>) => {
      if (!attachmentsEnabled) return
      e.preventDefault()
      dragDepth.current = 0
      setDropping(false)
      const files = e.dataTransfer?.files
      if (files && files.length > 0) ingestFiles(files)
    },
    [attachmentsEnabled, ingestFiles],
  )

  const mode: 'send' | 'stop' = isRunning ? 'stop' : 'send'
  const onActivate = mode === 'send' ? send : stop

  const pickerAccept = attachmentAdapter ? attachmentAdapter.accept ?? '*/*' : acceptImages

  const addAttachmentControl = attachmentsEnabled
    ? renderAddAttachment
      ? renderAddAttachment(openPicker)
      : (
        <button type="button" onClick={openPicker} aria-label="Add attachment">
          +
        </button>
      )
    : null

  const dictationButton = dictationSupported
    ? renderDictationButton
      ? renderDictationButton({ listening: dictating }, toggleDictation)
      : (
        <button
          type="button"
          onClick={toggleDictation}
          aria-label={dictating ? 'Stop dictation' : 'Start dictation'}
          aria-pressed={dictating}
        >
          {dictating ? '■' : '🎤'}
        </button>
      )
    : null

  return (
    <div
      data-jcode-ui=""
      className={className}
      data-running={isRunning ? 'true' : 'false'}
      data-dropping={dropping ? 'true' : 'false'}
      onDragEnter={attachmentsEnabled ? onDragEnter : undefined}
      onDragOver={attachmentsEnabled ? onDragOver : undefined}
      onDragLeave={attachmentsEnabled ? onDragLeave : undefined}
      onDrop={attachmentsEnabled ? onDrop : undefined}
    >
      {dropping && renderDropOverlay?.()}
      {renderQueue?.(queued)}
      {renderSlashMenu?.({
        open: !!slashOpen && filteredCommands.length > 0,
        commands: filteredCommands,
        activeIndex: slashOpen ? slashActive : -1,
        apply: applySlash,
      })}
      {allowImages && !attachmentAdapter && images.length > 0 && renderAttachments?.(images, removeImage)}
      {attachmentAdapter && pending.length > 0 && renderPendingAttachments?.(pendingItems)}
      <div className="jcode-composer-row" style={{ display: 'flex', alignItems: 'flex-end', gap: 8 }}>
        {renderPrefix?.()}
        {addAttachmentControl}
        {leadingControls}
        <textarea
          ref={textareaRef}
          value={text}
          onChange={(e) => setText(e.target.value)}
          onKeyDown={onKeyDown}
          onPaste={onPaste}
          placeholder={placeholder}
          aria-label={ariaLabel}
          rows={1}
          className="jcode-composer-input"
          style={{ resize: 'none', flex: 1, overflowY: 'auto' }}
        />
        {renderSuffix?.()}
        {dictationButton}
        {trailingControls}
        {renderSubmitButton
          ? renderSubmitButton(mode, mode === 'send' ? !canSend : false, onActivate)
          : (
            <button
              type="button"
              onClick={onActivate}
              disabled={mode === 'send' ? !canSend : false}
              aria-label={mode === 'send' ? 'Send message' : 'Stop'}
            >
              {mode === 'send' ? 'Send' : 'Stop'}
            </button>
          )}
      </div>
      {footer}
      {attachmentsEnabled && (
        <input
          ref={fileInputRef}
          type="file"
          accept={pickerAccept}
          multiple
          className="jcode-composer-file-input"
          style={{ display: 'none' }}
          aria-hidden
          tabIndex={-1}
          onChange={(e) => {
            const files = e.target.files
            if (files && files.length > 0) ingestFiles(files)
            e.target.value = ''
          }}
        />
      )}
    </div>
  )
})
