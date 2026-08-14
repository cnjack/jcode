import {
  ArrowPathIcon,
  CheckCircleIcon,
  ClockIcon,
  ExclamationTriangleIcon,
  SignalIcon,
  SignalSlashIcon,
  XMarkIcon,
} from '@heroicons/react/24/outline'
import { useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { modelRetryActions, remoteConnectionActions, retryRemoteConnection } from '../app/store'
import { useAppDispatch, useAppSelector } from '../app/hooks'
import type {
  ModelRetryNotice,
  RemoteConnectionNotice as RemoteConnectionNoticeState,
} from '../app/store'

const READY_VISIBLE_MS = 4_000

/** A task-scoped operational notice for SSH/Docker and model retry recovery.
 * It deliberately lives beside the composer instead of replacing the thread:
 * saved history stays readable and transport failures never masquerade as a
 * model/agent error in the transcript. */
export function RemoteConnectionNotice() {
  const { t } = useTranslation()
  const dispatch = useAppDispatch()
  const taskId = useAppSelector((s) => s.session.currentSessionId)
  const taskRunning = useAppSelector((s) => s.chat.isRunning)
  const notice = useAppSelector((s) => taskId ? s.remoteConnection.byTaskId[taskId] : undefined)
  const modelRetry = useAppSelector((s) => taskId ? s.modelRetry.byTaskId[taskId] : undefined)

  useEffect(() => {
    if (!notice || notice.status !== 'ready') return
    const timer = window.setTimeout(() => {
      dispatch(remoteConnectionActions.clear({ taskId: notice.task_id, revision: notice.revision }))
    }, READY_VISIBLE_MS)
    return () => window.clearTimeout(timer)
  }, [dispatch, notice])

  useEffect(() => {
    if (!modelRetry || modelRetry.status !== 'ready') return
    const timer = window.setTimeout(() => {
      dispatch(modelRetryActions.clear({ taskId: modelRetry.task_id, revision: modelRetry.revision }))
    }, READY_VISIBLE_MS)
    return () => window.clearTimeout(timer)
  }, [dispatch, modelRetry])

  if (!notice && !modelRetry) return null

  // A connection outage is the more fundamental blocker. Keep model retry
  // state alive underneath it so the right status appears if transport recovers.
  if (!notice && modelRetry) {
    const Icon = modelRetry.status === 'ready' ? CheckCircleIcon : ClockIcon
    return (
      <section
        className="remote-connection-notice remote-connection-notice--inline"
        data-status={modelRetry.status}
        role="status"
        aria-live="polite"
        aria-atomic="true"
        aria-label={t('modelRetry.label')}
      >
        <span key={modelRetry.revision} className="remote-connection-notice__inline-content">
          <Icon className="remote-connection-notice__inline-icon" aria-hidden="true" />
          <span className="remote-connection-notice__inline-copy">{modelRetryCopy(modelRetry, t)}</span>
        </span>
      </section>
    )
  }

  if (!notice) return null

  const inline = notice.status === 'waiting' || notice.status === 'reconnecting' || notice.status === 'ready'
  if (inline) {
    const Icon = notice.status === 'ready'
      ? CheckCircleIcon
      : notice.kind === 'ssh'
        ? notice.status === 'waiting' ? SignalSlashIcon : SignalIcon
        : notice.status === 'waiting' ? ClockIcon : ArrowPathIcon

    return (
      <section
        className="remote-connection-notice remote-connection-notice--inline"
        data-status={notice.status}
        role="status"
        aria-live="polite"
        aria-atomic="true"
        aria-label={t('remoteConnection.label')}
      >
        <span key={notice.revision} className="remote-connection-notice__inline-content">
          <Icon className="remote-connection-notice__inline-icon" aria-hidden="true" />
          <span className="remote-connection-notice__inline-copy">{inlineNoticeCopy(notice, t)}</span>
        </span>
      </section>
    )
  }

  const copy = noticeCopy(notice, t)
  const showAttempt = notice.attempt > 0 && notice.max_attempts > 0
  const outcomeUnknown = notice.code === 'remote_outcome_unknown'

  return (
    <section
      className="remote-connection-notice"
      data-status={notice.status}
      role="status"
      aria-live="polite"
      aria-atomic="true"
      aria-label={t('remoteConnection.label')}
    >
      <span className="remote-connection-notice__icon" aria-hidden="true">
        <ExclamationTriangleIcon className="h-4 w-4" />
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
      {(notice.status === 'failed' || notice.status === 'action_required') && (
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

function inlineNoticeCopy(notice: RemoteConnectionNoticeState, t: Translate): string {
  const options = {
    attempt: notice.attempt,
    max: notice.max_attempts,
    seconds: Math.max(1, Math.ceil((notice.retry_in_ms || 0) / 1_000)),
  }
  if (notice.status === 'waiting') {
    return notice.retry_in_ms
      ? t('remoteConnection.inline.waitingWithDelay', options)
      : t('remoteConnection.inline.waiting', options)
  }
  if (notice.status === 'reconnecting') {
    return t('remoteConnection.inline.reconnecting', options)
  }
  return t('remoteConnection.inline.ready')
}

function modelRetryCopy(notice: ModelRetryNotice, t: Translate): string {
  if (notice.status === 'ready') return t('modelRetry.ready')
  const options = {
    attempt: notice.attempt,
    max: notice.max_attempts,
    seconds: Math.max(1, Math.ceil((notice.retry_in_ms || 0) / 1_000)),
  }
  return notice.retry_in_ms
    ? t('modelRetry.waitingWithDelay', options)
    : t('modelRetry.waiting', options)
}

function noticeCopy(notice: RemoteConnectionNoticeState, t: Translate): { title: string; detail: string } {
  const detailOptions = {
    host: notice.host || t('remoteConnection.remoteHost'),
    transport: t(`remoteConnection.transport.${notice.kind}`),
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
