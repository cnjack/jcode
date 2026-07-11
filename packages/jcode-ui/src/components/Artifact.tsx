/**
 * Artifact — a titled card container for rich tool output (file viewers, diffs,
 * previews, generated documents…). Header with icon/title/subtitle + optional
 * actions slot + optional close button; a scrollable content region below.
 *
 * Built so the file-viewer / diff renderers can adopt it later without change
 * to their inner tables (this task only ships the container).
 */

import { memo } from 'react'
import type { ReactNode } from 'react'
import { XMarkIcon } from '@heroicons/react/24/outline'

export interface ArtifactProps {
  /** Primary label in the header. */
  title: string
  /** Secondary label (path, size, language…). Rendered muted + mono. */
  subtitle?: string
  /** Leading icon node (e.g. a heroicon or extension dot). */
  icon?: ReactNode
  /** Right-aligned actions (copy, download, open…). */
  actions?: ReactNode
  /** When provided, a close button appears at the far right. */
  onClose?: () => void
  /** Max height of the scrollable content region. Default '24rem'. */
  maxHeight?: string | number
  /** Extra classes on the root. */
  className?: string
  children: ReactNode
}

export const Artifact = memo(function Artifact({
  title,
  subtitle,
  icon,
  actions,
  onClose,
  maxHeight = '24rem',
  className,
  children,
}: ArtifactProps) {
  return (
    <div data-jcode-ui="" className={`jcode-artifact${className ? ` ${className}` : ''}`}>
      <div className="jcode-artifact__header">
        {icon && <span className="jcode-artifact__icon">{icon}</span>}
        <span className="jcode-artifact__title">{title}</span>
        {subtitle && <span className="jcode-artifact__subtitle">{subtitle}</span>}
        {(actions || onClose) && (
          <span className="jcode-artifact__actions">
            {actions}
            {onClose && (
              <button
                type="button"
                className="jcode-artifact__close"
                onClick={onClose}
                aria-label="Close"
              >
                <XMarkIcon className="jcode-artifact__close-icon" />
              </button>
            )}
          </span>
        )}
      </div>
      <div className="jcode-artifact__body" style={{ maxHeight }}>
        {children}
      </div>
    </div>
  )
})
