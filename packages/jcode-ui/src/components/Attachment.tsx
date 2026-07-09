/**
 * Attachment + AttachmentList — image-attachment thumbnails.
 *
 * Mirrors assistant-ui's Attachment component. Used inside ChatInput for paste/
 * picked images, but exported standalone so library users can compose their own
 * attachment UI (drag-drop zones, file pickers, etc.).
 */

import { memo } from 'react'
import { XMarkIcon } from '@heroicons/react/24/outline'
import type { ChatImage } from 'jcode-ui-core'

export interface AttachmentProps {
  image: ChatImage
  /** Optional remove handler — renders the × button when provided. */
  onRemove?: () => void
  /** Thumbnail size in px. Default 64. */
  size?: number
}

export const Attachment = memo(function Attachment({ image, onRemove, size = 64 }: AttachmentProps) {
  return (
    <div className="jcode-attachment relative inline-block" style={{ width: size, height: size }}>
      <img
        src={`data:${image.media_type};base64,${image.data}`}
        alt="attachment"
        className="h-full w-full rounded-[var(--radius-md)] border border-[var(--color-border)] object-cover"
      />
      {onRemove && (
        <button
          type="button"
          onClick={onRemove}
          className="absolute -right-1 -top-1 rounded-full bg-[var(--color-surface)] p-0.5 text-[var(--color-muted-foreground)] shadow-[var(--shadow-sm)] hover:text-[var(--color-foreground)]"
          aria-label="Remove attachment"
        >
          <XMarkIcon className="h-3 w-3" />
        </button>
      )}
    </div>
  )
})

export interface AttachmentListProps {
  images: ChatImage[]
  onRemove?: (index: number) => void
  size?: number
}

export const AttachmentList = memo(function AttachmentList({ images, onRemove, size }: AttachmentListProps) {
  if (!images || images.length === 0) return null
  return (
    <div className="jcode-attachment-list flex flex-wrap gap-1">
      {images.map((img, i) => (
        <Attachment key={i} image={img} size={size} onRemove={onRemove ? () => onRemove(i) : undefined} />
      ))}
    </div>
  )
})
