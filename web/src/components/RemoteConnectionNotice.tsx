import {
  ArrowPathIcon,
  CheckCircleIcon,
  ClockIcon,
  ExclamationTriangleIcon,
  XMarkIcon,
} from '@heroicons/react/24/outline'
import { useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { remoteConnectionActions, retryRemoteConnection } from '../app/store'
import { useAppDispatch, useAppSelector } from '../app/hooks'
import type { RemoteConnectionNotice as RemoteConnectionNoticeState } from '../app/store'

const READY_VISIBLE_MS = 4_000

/** A task-scoped operational notice for transparent SSH/Docker recovery.
 * It deliberately lives beside the composer instead of replacing the thread:
 * saved history stays readable and transport failures never masquerade as a
 * model/agent error in the transcript. */
export function RemoteConnectionNotice() {
  const { t } = useTranslation()
  const dispatch = useAppDispatch()
  const taskId = useAppSelector((s) => s.session.currentSessionId)
  const taskRunning = useAppSelector((s) => s.chat.isRunning)
  const notice = useAppSelector((s) => taskId ? s.remoteConnection.byTaskId[taskId] : undefined)

  useEffect(() => {
    if (!notice || notice.status !== 'ready') return
    const timer = window.setTimeout(() => {
      dispatch(remoteConnectionActions.clear({ taskId: notice.task_id, revision: notice.revision }))
    }, READY_VISIBLE_MS)
    return () => window.clearTimeout(timer)
  }, [dispatch, notice])

  if (!notice) return null

  const copy = noticeCopy(notice, t)
  const showProgress = notice.status === 'waiting' || notice.status === 'reconnecting'
  const showAttempt = notice.attempt > 0 && notice.max_attempts > 0
  const outcomeUnknown = notice.code === 'remote_outcome_unknown'
  const progress = showAttempt
    ? Math.min(100, Math.max(8, (notice.attempt / notice.max_attempts) * 100))
    : 8
  const Icon = notice.status === 'ready'
    ? CheckCircleIcon
    : notice.status === 'waiting'
      ? ClockIcon
      : notice.status === 'reconnecting'
        ? ArrowPathIcon
        : ExclamationTriangleIcon

  return (
    <section
      className="remote-connection-notice"
      data-status={notice.status}
      role="status"
      aria-live="polite"
      aria-atomic="true"
      aria-label={t('remoteConnection.label')}
    >
      {showProgress && (
        <div className="remote-connection-notice__track" aria-hidden="true">
          <span style={{ width: `${progress}%` }} />
        </div>
      )}
      <span className="remote-connection-notice__icon" aria-hidden="true">
        <Icon
          className={`h-4 w-4${notice.status === 'reconnecting' ? ' animate-spin motion-reduce:animate-none' : ''}`}
        />
      </span>
      <div className="remote-connection-notice__copy">
        <div className="remote-connection-notice__heading">
          <span>{copy.title}</span>
          {showAttempt && (
            <span className="remote-connection-notice__attempt">
              {t('remoteConnection.attempt', { attempt: notice.attempt, max: notice.max_attempts })}
            </span>
          )}
        </div>
        <p>{copy.detail}</p>
        {notice.error && (notice.status === 'failed' || notice.status === 'action_required') && (
          <details className="remote-connection-notice__details">
            <summary>{t('remoteConnection.details')}</summary>
            <p>{notice.error}</p>
          </details>
        )}
      </div>
      {(notice.status === 'ready' || notice.status === 'failed' || notice.status === 'action_required') && (
        <div className="remote-connection-notice__actions">
          {(notice.status === 'action_required' || (notice.status === 'failed' && notice.retryable !== false)) && (
            <button
              type="button"
              className="remote-connection-notice__retry"
              disabled={taskRunning}
              aria-busy={taskRunning}
              onClick={() => {
                if (outcomeUnknown) {
                  dispatch(remoteConnectionActions.clear({ taskId: notice.task_id, revision: notice.revision }))
                } else {
                  void dispatch(retryRemoteConnection({ taskId: notice.task_id }))
                }
              }}
            >
              <ArrowPathIcon className="h-3.5 w-3.5" />
              {outcomeUnknown
                ? t('remoteConnection.acknowledge')
                : taskRunning
                ? t('remoteConnection.waitForTurn')
                : notice.status === 'action_required'
                ? t('remoteConnection.resolve')
                : t('remoteConnection.retry')}
            </button>
          )}
          <button
            type="button"
            className="remote-connection-notice__dismiss"
            onClick={() => dispatch(remoteConnectionActions.clear({ taskId: notice.task_id, revision: notice.revision }))}
            aria-label={t('remoteConnection.dismiss')}
          >
            <XMarkIcon className="h-3.5 w-3.5" />
          </button>
        </div>
      )}
    </section>
  )
}

type Translate = (key: string, options?: Record<string, unknown>) => string

function noticeCopy(notice: RemoteConnectionNoticeState, t: Translate): { title: string; detail: string } {
  const detailOptions = {
    host: notice.host || t('remoteConnection.remoteHost'),
    transport: t(`remoteConnection.transport.${notice.kind}`),
    seconds: Math.max(1, Math.ceil((notice.retry_in_ms || 0) / 1_000)),
  }
  if (notice.status === 'waiting') {
    return {
      title: t('remoteConnection.waiting.title', detailOptions),
      detail: notice.retry_in_ms
        ? t('remoteConnection.waiting.withDelay', detailOptions)
        : t('remoteConnection.waiting.detail', detailOptions),
    }
  }
  if (notice.status === 'reconnecting') {
    return {
      title: t('remoteConnection.reconnecting.title', detailOptions),
      detail: t('remoteConnection.reconnecting.detail', detailOptions),
    }
  }
  if (notice.status === 'ready') {
    return {
      title: t('remoteConnection.ready.title', detailOptions),
      detail: t('remoteConnection.ready.detail', detailOptions),
    }
  }
  if (notice.status === 'action_required') {
    const codeKey = actionRequiredKey(notice.code)
    return {
      title: t(`remoteConnection.actionRequired.${codeKey}.title`, detailOptions),
      detail: t(`remoteConnection.actionRequired.${codeKey}.detail`, detailOptions),
    }
  }
  return {
    title: t('remoteConnection.failed.title', detailOptions),
    detail: t('remoteConnection.failed.detail', detailOptions),
  }
}

function actionRequiredKey(code?: string): 'auth' | 'unknownHost' | 'changedHost' | 'outcomeUnknown' | 'generic' {
  if (code === 'remote_outcome_unknown') return 'outcomeUnknown'
  if (code === 'ssh_auth_required') return 'auth'
  if (code === 'ssh_host_key_unknown') return 'unknownHost'
  if (code === 'ssh_host_key_changed' || code === 'ssh_host_key_confirmation_mismatch') return 'changedHost'
  return 'generic'
}
