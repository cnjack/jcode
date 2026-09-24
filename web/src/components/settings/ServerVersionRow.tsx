/**
 * ServerVersionRow — the General tab's "Version & updates" row for browser
 * hosts (`jcode web`). Desktop builds use VersionUpdateRow instead, which is
 * driven by the Tauri updater.
 *
 * Shows the running jcode build and, only when the user asks, checks the
 * latest GitHub release through the server (`/api/version?check=1`). A browser
 * host cannot swap the server binary itself, so an available update links to
 * the release notes and points at `jcode update`.
 */

import { useEffect, useRef, useState } from 'react'
import { ArrowPathIcon, ArrowTopRightOnSquareIcon } from '@heroicons/react/24/outline'
import { useTranslation } from 'react-i18next'
import { useAppSelector } from '../../app/hooks'
import { api, type VersionInfoResponse } from '../../lib/api'
import { openUrl } from '../../lib/useDesktop'
import { BTN_GHOST, BTN_SECONDARY, BTN_XS, ROW } from './atoms'

type CheckStatus = 'idle' | 'checking' | 'up-to-date' | 'available' | 'error'

/** Display form of a build version: numeric versions gain a single "v". */
export function formatVersion(raw: string | undefined): string {
  const v = (raw ?? '').trim()
  return /^\d/.test(v) ? `v${v}` : v
}

export function ServerVersionRow() {
  const { t } = useTranslation()
  const seededVersion = useAppSelector((s) => s.model.serverVersion)
  const [info, setInfo] = useState<VersionInfoResponse | null>(null)
  const [status, setStatus] = useState<CheckStatus>('idle')
  const mounted = useRef(true)

  useEffect(() => {
    mounted.current = true
    api
      .version()
      .then((res) => {
        // A manual check may have finished first; keep its richer result.
        if (mounted.current) setInfo((prev) => prev ?? res)
      })
      .catch(() => {})
    return () => {
      mounted.current = false
    }
  }, [])

  async function check() {
    setStatus('checking')
    try {
      const res = await api.version(true)
      if (!mounted.current) return
      setInfo(res)
      setStatus(res.check_error ? 'error' : res.update_available ? 'available' : 'up-to-date')
    } catch {
      if (mounted.current) setStatus('error')
    }
  }

  const version = formatVersion(info?.version || seededVersion)
  const commit = info?.git_commit
  const releaseURL = status === 'available' ? info?.release_url : undefined
  const hint =
    status === 'checking'
      ? t('update.checking')
      : status === 'up-to-date'
        ? t('update.upToDate')
        : status === 'available'
          ? t('update.newVersionFound', { version: formatVersion(info?.latest) })
          : status === 'error'
            ? t('update.checkFailed')
            : ''

  return (
    <div className={ROW} data-testid="server-version-row">
      <div className="grid h-7 w-7 shrink-0 place-items-center rounded-[var(--radius-md)] text-[var(--color-muted-foreground)]">
        <ArrowPathIcon className="h-4 w-4" />
      </div>
      <div className="min-w-0 flex-1">
        <div className="text-[12px] font-medium text-[var(--color-foreground)]">{t('update.sectionTitle')}</div>
        <div
          className="text-[11px] text-[var(--color-muted-foreground)]"
          aria-live="polite"
          title={info?.build_time || undefined}
        >
          {version ? `${t('update.currentVersion')} ${version}` : ''}
          {commit ? <span className="font-mono"> ({commit})</span> : null}
          {hint ? (version ? ` · ${hint}` : hint) : ''}
        </div>
        {status === 'available' && (
          <div className="mt-0.5 text-[11px] text-[var(--color-muted-foreground)]">
            {t('update.cliUpgradeHint')}{' '}
            <code className="rounded-[var(--radius-sm)] bg-[var(--color-muted)] px-1 font-mono text-[10px] text-[var(--color-foreground)]">
              jcode update
            </code>
          </div>
        )}
      </div>
      {releaseURL && (
        <button
          type="button"
          onClick={() => void openUrl(releaseURL).catch(() => {})}
          className={`${BTN_GHOST} ${BTN_XS}`}
        >
          {t('update.releaseNotes')}
          <ArrowTopRightOnSquareIcon className="h-3 w-3" />
        </button>
      )}
      <button
        type="button"
        onClick={() => void check()}
        disabled={status === 'checking'}
        className={`${BTN_SECONDARY} ${BTN_XS}`}
      >
        {t('update.checkButton')}
      </button>
    </div>
  )
}
