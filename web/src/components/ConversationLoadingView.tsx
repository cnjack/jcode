import { useEffect, useMemo, useRef, useState } from 'react'
import {
  ArrowPathIcon,
  CheckCircleIcon,
  CubeIcon,
  ExclamationTriangleIcon,
  KeyIcon,
  ServerIcon,
  ShieldCheckIcon,
} from '@heroicons/react/24/outline'
import { RuntimeProvider, Thread, createMockRuntime } from 'jcode-ui'
import { useTranslation } from 'react-i18next'
import { useAppDispatch, useAppSelector } from '../app/hooks'
import {
  cancelConversationLoad,
  continueConversationLoad,
  type ConversationRemoteCredentials,
} from '../app/store'

/** Dedicated foreground for opening an existing conversation. It keeps the
 * previous committed chat untouched while a read-only history runtime can
 * paint as soon as GET /api/sessions/{id} completes. */
export function ConversationLoadingView() {
  const { t } = useTranslation()
  const dispatch = useAppDispatch()
  const load = useAppSelector((state) => state.conversationLoad)
  const [authMethod, setAuthMethod] = useState<'key' | 'password'>('key')
  const [password, setPassword] = useState('')
  const [keyPath, setKeyPath] = useState('~/.ssh/id_rsa')
  const [passphrase, setPassphrase] = useState('')
  const previewRef = useRef<HTMLDivElement | null>(null)

  useEffect(() => {
    setAuthMethod('key')
    setPassword('')
    setKeyPath('~/.ssh/id_rsa')
    setPassphrase('')
  }, [load.requestId])

  useEffect(() => {
    const preview = previewRef.current
    if (!preview) return
    // Native inert blocks pointer, keyboard and focus interaction in every
    // package renderer without needing package-specific no-op callbacks.
    preview.setAttribute('inert', '')
    return () => preview.removeAttribute('inert')
  }, [load.historyStatus])

  const previewRuntime = useMemo(
    // `isRunning` keeps jcode-ui from offering "Edit" on historical user
    // turns. The pending slot is suppressed below: this runtime is a read-only
    // preview, not an agent run.
    () => createMockRuntime({
      items: load.previewTimeline.filter((item) => item.kind !== 'approval' || !!item.data.resolved),
      isRunning: true,
    }),
    [load.previewTimeline],
  )
  const remoteKind = load.target?.project.startsWith('docker://') ? 'docker'
    : load.target?.project.startsWith('ssh://') ? 'ssh' : 'local'
  const PhaseIcon = remoteKind === 'docker' ? CubeIcon : remoteKind === 'ssh' ? ServerIcon : ArrowPathIcon
  const phaseLabel = load.phase === 'awaiting_host_key' || load.phase === 'awaiting_auth'
    ? t('conversationLoading.phase.actionRequired')
    : load.phase === 'error'
      ? t('conversationLoading.phase.failed')
      : load.phase === 'connecting'
    ? t(`conversationLoading.phase.${remoteKind === 'docker' ? 'docker' : 'ssh'}`)
    : load.phase === 'activating'
      ? t('conversationLoading.phase.activating')
      : t('conversationLoading.phase.history')
  const credentials: ConversationRemoteCredentials = {
    authMethod,
    password: authMethod === 'password' ? password : undefined,
    keyPath: authMethod === 'key' ? keyPath : undefined,
    passphrase: authMethod === 'key' ? passphrase : undefined,
  }

  function cancel() {
    void dispatch(cancelConversationLoad())
  }

  function retry(options?: { acceptHostKey?: boolean; includeCredentials?: boolean }) {
    void dispatch(continueConversationLoad({
      requestId: load.requestId,
      acceptHostKey: options?.acceptHostKey,
      credentials: options?.includeCredentials ? credentials : undefined,
    }))
  }

  const hostKey = load.hostKey
  const unknownHost = hostKey?.code === 'ssh_host_key_unknown'
  const changedHost = hostKey?.code === 'ssh_host_key_changed'

  return (
    <div className="chat-panel conversation-loading" role="region" aria-label={t('conversationLoading.title')}>
      <header className="conversation-loading__header">
        <div className={`conversation-loading__phase-icon${load.phase === 'error' ? ' is-error' : ''}`}>
          {load.phase === 'error'
            ? <ExclamationTriangleIcon className="h-5 w-5" />
            : <PhaseIcon className={`h-5 w-5${load.phase === 'awaiting_host_key' || load.phase === 'awaiting_auth' ? '' : ' spin'}`} />}
        </div>
        <div className="min-w-0">
          <h1 className="conversation-loading__title">
            {load.target?.title || t('conversationLoading.title')}
          </h1>
          <p className="conversation-loading__phase" aria-live="polite">{phaseLabel}</p>
        </div>
        <div className="conversation-loading__steps" aria-label={t('conversationLoading.progress')}>
          <StatusStep ready={load.historyStatus === 'ready'} active={load.historyStatus === 'loading'} label={t('conversationLoading.steps.history')} />
          <StatusStep ready={load.environmentStatus === 'ready'} active={load.environmentStatus === 'loading'} label={t('conversationLoading.steps.environment')} />
          <StatusStep ready={false} active={load.phase === 'activating'} label={t('conversationLoading.steps.session')} />
        </div>
      </header>

      {(load.phase === 'awaiting_host_key' || load.phase === 'awaiting_auth' || load.phase === 'error') && (
        <section className={`conversation-loading__action${changedHost ? ' is-danger' : ''}`} aria-live="polite">
          {load.phase === 'awaiting_host_key' && hostKey ? (
            <>
              <div className="conversation-loading__action-heading">
                {unknownHost ? <ShieldCheckIcon className="h-5 w-5" /> : <ExclamationTriangleIcon className="h-5 w-5" />}
                <div>
                  <h2>{unknownHost ? t('conversationLoading.hostKey.unknownTitle') : changedHost ? t('conversationLoading.hostKey.changedTitle') : t('conversationLoading.hostKey.mismatchTitle')}</h2>
                  <p>{unknownHost ? t('conversationLoading.hostKey.unknownBody') : changedHost ? t('conversationLoading.hostKey.changedBody') : t('conversationLoading.hostKey.mismatchBody')}</p>
                </div>
              </div>
              <dl className="conversation-loading__fingerprint">
                <div><dt>{t('conversationLoading.hostKey.host')}</dt><dd>{hostKey.host}</dd></div>
                <div><dt>{t('conversationLoading.hostKey.keyType')}</dt><dd>{hostKey.key_type}</dd></div>
                {hostKey.old_fingerprint && (
                  <div><dt>{t('conversationLoading.hostKey.previous')}</dt><dd>{hostKey.old_fingerprint}</dd></div>
                )}
                {hostKey.expected_fingerprint && (
                  <div><dt>{t('conversationLoading.hostKey.expected')}</dt><dd>{hostKey.expected_fingerprint}</dd></div>
                )}
                <div><dt>{t('conversationLoading.hostKey.presented')}</dt><dd>{hostKey.fingerprint}</dd></div>
              </dl>
              <div className="conversation-loading__buttons">
                <button type="button" className="conversation-loading__secondary" onClick={cancel}>{t('common.cancel')}</button>
                {unknownHost ? (
                  <button type="button" className="conversation-loading__primary" onClick={() => retry({ acceptHostKey: true })}>
                    <ShieldCheckIcon className="h-4 w-4" /> {t('conversationLoading.hostKey.accept')}
                  </button>
                ) : !changedHost ? (
                  <button type="button" className="conversation-loading__primary" onClick={() => retry()}>
                    <ArrowPathIcon className="h-4 w-4" /> {t('common.retry')}
                  </button>
                ) : null}
              </div>
            </>
          ) : load.phase === 'awaiting_auth' ? (
            <>
              <div className="conversation-loading__action-heading">
                <KeyIcon className="h-5 w-5" />
                <div>
                  <h2>{t('conversationLoading.auth.title')}</h2>
                  <p>{load.error || t('conversationLoading.auth.body')}</p>
                </div>
              </div>
              <div className="conversation-loading__auth">
                <div className="conversation-loading__segmented" role="group" aria-label={t('wizard.auth')}>
                  <button type="button" className={authMethod === 'key' ? 'is-active' : ''} onClick={() => setAuthMethod('key')}>{t('wizard.authKey')}</button>
                  <button type="button" className={authMethod === 'password' ? 'is-active' : ''} onClick={() => setAuthMethod('password')}>{t('wizard.authPassword')}</button>
                </div>
                {authMethod === 'key' ? (
                  <div className="conversation-loading__field-row">
                    <label><span>{t('wizard.keyPath')}</span><input value={keyPath} onChange={(event) => setKeyPath(event.target.value)} /></label>
                    <label><span>{t('wizard.passphrase')}</span><input type="password" value={passphrase} onChange={(event) => setPassphrase(event.target.value)} /></label>
                  </div>
                ) : (
                  <label><span>{t('wizard.password')}</span><input type="password" value={password} onChange={(event) => setPassword(event.target.value)} autoFocus /></label>
                )}
              </div>
              <div className="conversation-loading__buttons">
                <button type="button" className="conversation-loading__secondary" onClick={cancel}>{t('common.cancel')}</button>
                <button type="button" className="conversation-loading__primary" onClick={() => retry({ includeCredentials: true })}>
                  <ArrowPathIcon className="h-4 w-4" /> {t('conversationLoading.auth.reconnect')}
                </button>
              </div>
            </>
          ) : (
            <>
              <div className="conversation-loading__action-heading">
                <ExclamationTriangleIcon className="h-5 w-5" />
                <div><h2>{t('conversationLoading.error.title')}</h2><p>{load.error}</p></div>
              </div>
              <div className="conversation-loading__buttons">
                <button type="button" className="conversation-loading__secondary" onClick={cancel}>{t('common.cancel')}</button>
                {load.retryable && (
                  <button type="button" className="conversation-loading__primary" onClick={() => retry()}>
                    <ArrowPathIcon className="h-4 w-4" /> {t('common.retry')}
                  </button>
                )}
              </div>
            </>
          )}
        </section>
      )}

      <div className="conversation-loading__history">
        {load.historyStatus === 'ready' ? (
          <>
            <div className="conversation-loading__history-label">
              <CheckCircleIcon className="h-4 w-4" /> {t('conversationLoading.historyReady')}
            </div>
            <div ref={previewRef} className="conversation-loading__preview" aria-label={t('conversationLoading.readOnlyHistory')} aria-readonly="true">
              <RuntimeProvider runtime={previewRuntime}>
                <Thread overscanBottom={6} renderPending={() => null} hidePendingAskUser />
              </RuntimeProvider>
            </div>
          </>
        ) : (
          <ConversationHistorySkeleton label={t('conversationLoading.loadingHistory')} />
        )}
      </div>
    </div>
  )
}

function StatusStep({ ready, active, label }: { ready: boolean; active: boolean; label: string }) {
  return (
    <span className={`conversation-loading__step${ready ? ' is-ready' : ''}${active ? ' is-active' : ''}`}>
      <span className="conversation-loading__step-dot">{ready ? <CheckCircleIcon className="h-3.5 w-3.5" /> : null}</span>
      {label}
    </span>
  )
}

function ConversationHistorySkeleton({ label }: { label: string }) {
  return (
    <div className="resume-skeleton jcode-chat-col" role="status" aria-live="polite" aria-label={label}>
      <div className="resume-skeleton__rows jcode-gutter">
        <div className="rs-row rs-row--user"><div className="rs-bar" style={{ width: '46%' }} /></div>
        <div className="rs-row"><div className="rs-bar" style={{ width: '78%' }} /></div>
        <div className="rs-row"><div className="rs-bar" style={{ width: '62%' }} /></div>
        <div className="rs-row rs-row--user"><div className="rs-bar" style={{ width: '38%' }} /></div>
      </div>
      <span className="resume-skeleton__label">{label}</span>
    </div>
  )
}
