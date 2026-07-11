/**
 * Attachment + AttachmentList — image attachment thumbnails.
 *
 * Aligns with assistant-ui's attachment UI (composer strip + remove + preview),
 * scoped to vision images (`ChatImage` base64). Used by ChatInput and Message;
 * product web reuses the same components for visual parity.
 */

import { memo, useCallback, useEffect, useId, useState } from 'react'
import { createPortal } from 'react-dom'
import { ArrowPathIcon, DocumentIcon, XMarkIcon } from '@heroicons/react/24/outline'
import type { ChatImage } from 'jcode-ui-core'
import type { PendingAttachmentItem } from 'jcode-ui-core/primitives'

function imageSrc(image: ChatImage): string {
  const data = image.data?.trim() ?? ''
  if (!data) return ''
  if (data.startsWith('data:')) return data
  // Percent-encoded SVG payloads (rare host form).
  if (data.startsWith('<svg') || data.startsWith('<?xml')) {
    return `data:${image.media_type || 'image/svg+xml'};charset=utf-8,${encodeURIComponent(data)}`
  }
  return `data:${image.media_type || 'image/*'};base64,${data}`
}

export interface AttachmentProps {
  image: ChatImage
  /** Optional remove handler — renders the × button when provided (composer). */
  onRemove?: () => void
  /** Thumbnail size in px. Default 56 (assistant-ui tile is ~56). */
  size?: number
  /** Allow click-to-preview lightbox. Default true. */
  preview?: boolean
}

export const Attachment = memo(function Attachment({
  image,
  onRemove,
  size = 56,
  preview = true,
}: AttachmentProps) {
  const [open, setOpen] = useState(false)
  const [broken, setBroken] = useState(false)
  const title = image.name || 'Image attachment'
  const src = imageSrc(image)
  const dialogTitleId = useId()

  const close = useCallback(() => setOpen(false), [])

  useEffect(() => {
    setBroken(false)
  }, [src])

  useEffect(() => {
    if (!open) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') close()
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [close, open])

  return (
    <>
      <div
        data-jcode-ui="" className="jcode-attachment"
        style={{ width: size, height: size }}
        title={title}
      >
        <button
          type="button"
          className="jcode-attachment__thumb"
          onClick={() => preview && !broken && setOpen(true)}
          aria-label={preview ? `Preview ${title}` : title}
          disabled={!preview || broken || !src}
        >
          {!src || broken ? (
            <span
              aria-hidden
              style={{
                display: 'flex',
                height: '100%',
                width: '100%',
                alignItems: 'center',
                justifyContent: 'center',
                fontSize: 10,
                color: 'var(--jcode-color-muted-foreground)',
                background: 'var(--jcode-color-muted)',
              }}
            >
              img
            </span>
          ) : (
            <img
              src={src}
              alt={title}
              draggable={false}
              onError={() => setBroken(true)}
            />
          )}
        </button>
        {onRemove && (
          <button
            type="button"
            className="jcode-attachment__remove"
            onClick={(e) => {
              e.preventDefault()
              e.stopPropagation()
              onRemove()
            }}
            aria-label={`Remove ${title}`}
          >
            <XMarkIcon />
          </button>
        )}
      </div>

      {open &&
        src &&
        !broken &&
        typeof document !== 'undefined' &&
        createPortal(
          <div
            data-jcode-ui="" className="jcode-attachment-preview"
            role="dialog"
            aria-modal="true"
            aria-labelledby={dialogTitleId}
            onClick={close}
          >
            <div className="jcode-attachment-preview__scrim" />
            <div
              className="jcode-attachment-preview__panel"
              onClick={(e) => e.stopPropagation()}
            >
              <div className="jcode-attachment-preview__bar">
                <span id={dialogTitleId} className="jcode-attachment-preview__name">
                  {title}
                </span>
                <button
                  type="button"
                  className="jcode-attachment-preview__close"
                  onClick={close}
                  aria-label="Close preview"
                >
                  <XMarkIcon className="h-4 w-4" />
                </button>
              </div>
              <img src={src} alt={title} className="jcode-attachment-preview__img" />
            </div>
          </div>,
          document.body,
        )}
    </>
  )
})

export interface AttachmentListProps {
  images: ChatImage[]
  onRemove?: (index: number) => void
  size?: number
  /** Click-to-preview. Default true. */
  preview?: boolean
  className?: string
}

export const AttachmentList = memo(function AttachmentList({
  images,
  onRemove,
  size,
  preview = true,
  className,
}: AttachmentListProps) {
  if (!images || images.length === 0) return null
  return (
    <div
      data-jcode-ui="" className={['jcode-attachment-list', className].filter(Boolean).join(' ')}
      data-count={images.length}
    >
      {images.map((img, i) => (
        <Attachment
          key={`${img.media_type}-${img.name ?? i}-${String(img.data).slice(0, 24)}-${i}`}
          image={img}
          size={size}
          preview={preview}
          onRemove={onRemove ? () => onRemove(i) : undefined}
        />
      ))}
    </div>
  )
})

// ─── Pending attachments (adapter path) ──────────────────────────────────────

function fileExt(name: string): string {
  const dot = name.lastIndexOf('.')
  if (dot <= 0 || dot === name.length - 1) return ''
  return name.slice(dot + 1).toUpperCase()
}

function formatBytes(bytes?: number): string {
  if (bytes == null || bytes <= 0) return ''
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(0)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}

/** One pending attachment: image tile (with data) or a file chip. */
const PendingTile = memo(function PendingTile({ item, size = 56 }: { item: PendingAttachmentItem; size?: number }) {
  const { attachment: a, status, remove, retry } = item
  const isError = status === 'error'
  const isUploading = status === 'uploading'
  const pct = Math.round((a.progress ?? 0) * 100)

  // Image with inline data → thumbnail tile with an overlay.
  if (a.kind === 'image' && a.data) {
    const src = imageSrc({ data: a.data, media_type: a.media_type || 'image/*', name: a.name })
    return (
      <div
        className={`jcode-pending-image${isError ? ' is-error' : ''}`}
        style={{ width: size, height: size }}
        title={a.error || a.name}
      >
        <img src={src} alt={a.name} draggable={false} />
        {isUploading && (
          <span className="jcode-pending-image__veil" aria-hidden>
            <span className="jcode-pending-spinner" />
          </span>
        )}
        {isError && (
          <button type="button" className="jcode-pending-image__retry" onClick={retry} aria-label="Retry upload">
            <ArrowPathIcon />
          </button>
        )}
        <button type="button" className="jcode-attachment__remove" onClick={remove} aria-label={`Remove ${a.name}`}>
          <XMarkIcon />
        </button>
      </div>
    )
  }

  // Otherwise → file chip with icon + name + size + progress.
  const ext = fileExt(a.name)
  return (
    <div className={`jcode-attachment-chip${isError ? ' is-error' : ''}`} title={a.error || a.name}>
      <span className="jcode-attachment-chip__icon" aria-hidden>
        <DocumentIcon />
        {ext && <span className="jcode-attachment-chip__ext">{ext}</span>}
      </span>
      <span className="jcode-attachment-chip__meta">
        <span className="jcode-attachment-chip__name">{a.name}</span>
        <span className="jcode-attachment-chip__sub">
          {isError ? a.error || 'Failed' : isUploading ? `Uploading… ${pct}%` : formatBytes(a.size)}
        </span>
        {isUploading && (
          <span className="jcode-attachment-progress" aria-hidden>
            <span className="jcode-attachment-progress__fill" style={{ width: `${pct}%` }} />
          </span>
        )}
      </span>
      {isError && (
        <button type="button" className="jcode-attachment-chip__retry" onClick={retry} aria-label="Retry upload">
          <ArrowPathIcon />
        </button>
      )}
      <button
        type="button"
        className="jcode-attachment-chip__remove"
        onClick={remove}
        aria-label={`Remove ${a.name}`}
      >
        <XMarkIcon />
      </button>
    </div>
  )
})

export interface PendingAttachmentListProps {
  items: PendingAttachmentItem[]
  /** Image tile size in px. Default 56. */
  size?: number
  className?: string
}

/** Renders the composer's pending-attachment strip (Composer 2 adapter path). */
export const PendingAttachmentList = memo(function PendingAttachmentList({
  items,
  size,
  className,
}: PendingAttachmentListProps) {
  if (!items || items.length === 0) return null
  return (
    <div
      data-jcode-ui=""
      className={['jcode-pending-attachments', className].filter(Boolean).join(' ')}
      data-count={items.length}
    >
      {items.map((item) => (
        <PendingTile key={item.attachment.id} item={item} size={size} />
      ))}
    </div>
  )
})
