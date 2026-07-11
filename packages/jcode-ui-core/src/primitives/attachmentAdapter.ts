/**
 * AttachmentAdapter — the pluggable contract for composer attachments.
 *
 * The headless `Composer` owns the pending-attachment state machine
 * (uploading → done / error, progress passthrough). *How* a raw `File` becomes
 * a sendable attachment is delegated to an adapter, so hosts can choose between:
 *
 *   - inline base64 images (vision fast-path — `createInlineImageAdapter`), or
 *   - an upload adapter that PUTs to object storage and returns a host URL, or
 *   - a hybrid that inlines small images and uploads everything else.
 *
 * An adapter never touches React; it takes a `File`, optionally reports
 * progress, and resolves a `PendingAttachment`. A failed attachment resolves
 * with `error` set (rather than rejecting) so the composer can render it inline
 * with a retry affordance — a thrown/rejected error is also tolerated by the
 * composer and surfaced the same way.
 */

/** One attachment in the composer's pending strip. */
export interface PendingAttachment {
  /** Stable id (adapter- or composer-assigned) for keying, remove, retry. */
  id: string
  /** Coarse kind — drives tile (image) vs. chip (file) presentation. */
  kind: 'image' | 'file'
  /** Display name (usually the original filename). */
  name: string
  /** Size in bytes when known. */
  size?: number
  /** MIME type when known. */
  media_type?: string
  /** base64 payload for images (ChatImage fast-path, no `data:` prefix). */
  data?: string
  /** Host URL once uploaded (upload adapters). */
  url?: string
  /** Upload progress in the range 0–1. */
  progress?: number
  /** Failure message; when set the attachment is in the error state. */
  error?: string
}

/**
 * Turns raw `File`s into `PendingAttachment`s. Supplied to `Composer` via the
 * `attachmentAdapter` prop.
 */
export interface AttachmentAdapter {
  /** `accept` attribute for the file picker (default falls back to `*​/*`). */
  accept?: string
  /**
   * Ingest a file. `onProgress` (0–1) may be called any number of times before
   * the promise settles. Resolve with `error` set for a handled failure.
   */
  add(file: File, onProgress?: (p: number) => void): Promise<PendingAttachment>
  /** Optional cleanup when an attachment is removed (e.g. delete an upload). */
  remove?(id: string): void | Promise<void>
}

let idCounter = 0

/** Monotonic, collision-resistant id for composer-assigned attachments. */
export function nextAttachmentId(): string {
  idCounter += 1
  return `att_${Date.now().toString(36)}_${idCounter.toString(36)}`
}

/** Options for {@link createInlineImageAdapter}. */
export interface InlineImageAdapterOptions {
  /** Reject images larger than this (bytes). Default 10MB. */
  maxBytes?: number
  /** `accept` for the picker. Default `image/*`. */
  accept?: string
}

/**
 * The default adapter: reads image files into base64 (the existing ChatImage
 * behavior) and reports an error for non-images or oversize files. Keeps the
 * `sendMessage(text, images)` fast-path working with zero host wiring.
 */
export function createInlineImageAdapter(options: InlineImageAdapterOptions = {}): AttachmentAdapter {
  const maxBytes = options.maxBytes ?? 10 * 1024 * 1024
  return {
    accept: options.accept ?? 'image/*',
    add(file: File): Promise<PendingAttachment> {
      const base: PendingAttachment = {
        id: nextAttachmentId(),
        kind: file.type.startsWith('image/') ? 'image' : 'file',
        name: file.name || 'attachment',
        size: file.size,
        media_type: file.type || undefined,
      }
      if (!file.type.startsWith('image/')) {
        return Promise.resolve({ ...base, error: 'Only image files are supported' })
      }
      if (file.size > maxBytes) {
        return Promise.resolve({ ...base, error: 'Image exceeds the size limit' })
      }
      return new Promise<PendingAttachment>((resolve) => {
        const reader = new FileReader()
        reader.onload = () => {
          const dataUrl = String(reader.result ?? '')
          const comma = dataUrl.indexOf(',')
          if (comma < 0) {
            resolve({ ...base, error: 'Could not read image' })
            return
          }
          resolve({ ...base, data: dataUrl.slice(comma + 1), progress: 1 })
        }
        reader.onerror = () => resolve({ ...base, error: 'Could not read image' })
        reader.readAsDataURL(file)
      })
    },
  }
}
