/**
 * UpdateBanner — desktop auto-update toast, floating bottom-right. It is the
 * only visible surface of the useAppUpdate flow and is mounted once by App's
 * Shell so it stays reachable from every workspace view. Renders nothing in
 * the browser bundle or once dismissed (Settings → "check for updates" can
 * bring it back by finding a newer version again).
 */

import { ArrowPathIcon, ArrowUpTrayIcon, XMarkIcon } from '@heroicons/react/24/outline'
import { useTranslation } from 'react-i18next'
import { useAppUpdate } from '../lib/useAppUpdate'
import { isTauri } from '../lib/useDesktop'

export function UpdateBanner() {
  const { t } = useTranslation()
  const { status, version, notes, progress, error, dismissed, install, dismiss } = useAppUpdate()

  if (!isTauri || dismissed) return null
  // Passive states stay invisible; manual-check feedback lives in Settings.
  if (status === 'idle' || status === 'checking' || status === 'up-to-date') return null

  const pct = progress > 0 ? Math.round(progress * 100) : null

  return (
    <div
      role="status"
      className="fixed bottom-4 right-4 z-50 flex w-[340px] flex-col gap-2.5 rounded-[var(--radius-2xl)] border border-[var(--color-border)] bg-[var(--color-surface)] p-4 shadow-[var(--shadow-sm)]"
    >
      <div className="flex items-start gap-2.5">
        {status === 'error' ? (
          <ArrowPathIcon className="mt-0.5 h-4 w-4 shrink-0 text-[var(--color-destructive)]" />
        ) : (
          <ArrowUpTrayIcon className="mt-0.5 h-4 w-4 shrink-0 text-[var(--color-primary)]" />
        )}
        <div className="min-w-0 flex-1">
          <p className="text-[13px] font-semibold text-[var(--color-foreground)]">
            {status === 'available' && t('update.availableTitle')}
            {status === 'downloading' && t('update.downloading')}
            {status === 'restarting' && t('update.restarting')}
            {status === 'error' && t('update.failed')}
          </p>
          {status === 'available' && !!version && (
            <p className="mt-0.5 text-[12px] text-[var(--color-muted-foreground)]">
              {t('update.updateTo', { version })}
            </p>
          )}
          {status === 'available' && !!notes && (
            <p className="mt-1.5 line-clamp-3 whitespace-pre-line text-[12px] leading-relaxed text-[var(--color-muted-foreground)]">
              {notes}
            </p>
          )}
          {status === 'error' && !!error && (
            <p className="mt-1.5 line-clamp-3 text-[12px] leading-relaxed text-[var(--color-error-fg)]">{error}</p>
          )}
        </div>
        {(status === 'available' || status === 'error') && (
          <button
            type="button"
            aria-label={t('update.dismiss')}
            onClick={dismiss}
            className="rounded-[var(--radius-md)] p-1 text-[var(--color-muted-foreground)] transition-colors hover:bg-[var(--color-secondary)] hover:text-[var(--color-foreground)]"
          >
            <XMarkIcon className="h-3.5 w-3.5" />
          </button>
        )}
      </div>

      {(status === 'downloading' || status === 'restarting') && (
        <div className="flex flex-col gap-1">
          <div className="h-1.5 w-full overflow-hidden rounded-full bg-[var(--color-secondary)]">
            <div
              className="h-full rounded-full bg-[var(--color-primary)] transition-[width] duration-200"
              style={{ width: `${status === 'restarting' ? 100 : Math.max(progress * 100, 6)}%` }}
            />
          </div>
          <p className="text-right text-[11px] tabular-nums text-[var(--color-muted-foreground)]">
            {pct !== null ? `${pct}%` : ''}
          </p>
        </div>
      )}

      {status === 'available' && (
        <div className="flex items-center justify-end gap-2">
          <button
            type="button"
            onClick={dismiss}
            className="rounded-[var(--radius-md)] px-2.5 py-1.5 text-[12px] font-medium text-[var(--color-muted-foreground)] transition-colors hover:bg-[var(--color-secondary)] hover:text-[var(--color-foreground)]"
          >
            {t('update.later')}
          </button>
          <button
            type="button"
            onClick={() => void install()}
            className="rounded-[var(--radius-md)] bg-[var(--color-primary)] px-3 py-1.5 text-[12px] font-semibold text-[var(--color-on-primary)] transition-opacity hover:opacity-90"
          >
            {t('update.updateRestart')}
          </button>
        </div>
      )}

      {status === 'error' && (
        <div className="flex items-center justify-end gap-2">
          <button
            type="button"
            onClick={() => void install()}
            className="inline-flex items-center gap-1.5 rounded-[var(--radius-md)] bg-[var(--color-primary)] px-3 py-1.5 text-[12px] font-semibold text-[var(--color-on-primary)] transition-opacity hover:opacity-90"
          >
            <ArrowPathIcon className="h-3.5 w-3.5" />
            {t('update.retry')}
          </button>
        </div>
      )}
    </div>
  )
}
