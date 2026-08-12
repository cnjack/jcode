/**
 * RemoteConnectWizard — SSH / Docker connect flow.
 * Parity with web/src/components/RemoteConnectWizard.vue:
 *   method → config|docker → connecting → dir → bind
 *   + prefill silent reconnect for key/agent and docker
 */

import { useEffect, useMemo, useRef, useState } from 'react'
import {
  ArrowLeftIcon,
  ArrowPathIcon,
  CheckIcon,
  ChevronDownIcon,
  ChevronRightIcon,
  CubeIcon,
  FolderIcon,
  ServerIcon,
  XMarkIcon,
} from '@heroicons/react/24/outline'
import { useTranslation } from 'react-i18next'
import { useAppDispatch } from '../app/hooks'
import { chatActions, loadWorkspaceState, sessionActions } from '../app/store'
import { api, isAPIError } from '../lib/api'
import { sshReconnectRequest, type RemotePrefill } from '../lib/remote'
import type { DockerContainer, RemoteAuthMethod, RemoteHostKeyErrorPayload, RemoteKind, SSHAlias } from '../lib/types'

type Step = 'method' | 'config' | 'docker' | 'connecting' | 'dir'

export interface RemoteConnectWizardProps {
  open: boolean
  prefill?: RemotePrefill | null
  onClose: () => void
  onBound?: () => void
}

export function RemoteConnectWizard({ open, prefill, onClose, onBound }: RemoteConnectWizardProps) {
  const { t } = useTranslation()
  const dispatch = useAppDispatch()
  const [step, setStep] = useState<Step>('method')
  const [method, setMethod] = useState<RemoteKind>('ssh')
  const [host, setHost] = useState('')
  const [port, setPort] = useState(22)
  const [user, setUser] = useState('root')
  const [authMethod, setAuthMethod] = useState<RemoteAuthMethod>('key')
  const [password, setPassword] = useState('')
  const [keyPath, setKeyPath] = useState('~/.ssh/id_rsa')
  const [passphrase, setPassphrase] = useState('')
  const [aliases, setAliases] = useState<SSHAlias[]>([])
  const [selectedAlias, setSelectedAlias] = useState('')
  const [containers, setContainers] = useState<DockerContainer[]>([])
  const [containersLoading, setContainersLoading] = useState(false)
  const [connectionId, setConnectionId] = useState('')
  const [currentDir, setCurrentDir] = useState('')
  const [dirs, setDirs] = useState<string[]>([])
  const [dirLoading, setDirLoading] = useState(false)
  const [aliasName, setAliasName] = useState('')
  const [error, setError] = useState('')
  const [hostKeyPrompt, setHostKeyPrompt] = useState<RemoteHostKeyErrorPayload | null>(null)
  const [binding, setBinding] = useState(false)
  const [aliasMenuOpen, setAliasMenuOpen] = useState(false)
  const boundRef = useRef(false)
  const aliasMenuRef = useRef<HTMLDivElement | null>(null)
  const hostKeyRetryRef = useRef<((fingerprint?: string) => Promise<void>) | null>(null)

  const steps = useMemo(
    () => [
      { key: 'method' as const, label: t('wizard.steps.chooseMethod') },
      { key: 'config' as const, label: t('wizard.steps.configure') },
      { key: 'connecting' as const, label: t('wizard.steps.connecting') },
      { key: 'dir' as const, label: t('wizard.steps.selectDirectory') },
    ],
    [t],
  )
  const stepIndex = useMemo(() => {
    const key = step === 'docker' ? 'config' : step
    return steps.findIndex((s) => s.key === key)
  }, [step, steps])

  const selectedAliasLabel = useMemo(() => {
    if (!selectedAlias) return t('settings.ssh.noAlias')
    const alias = aliases.find((a) => a.name === selectedAlias)
    if (!alias) return selectedAlias
    return `${alias.name} — ${alias.addr}`
  }, [selectedAlias, aliases, t])

  useEffect(() => {
    if (!open) return
    boundRef.current = false
    resetForm()
    void loadAliases()
    if (prefill) {
      if (prefill.kind === 'docker') {
        void autoReconnectDocker(prefill)
      } else if (prefill.remotePath) {
        void autoReconnectSSH(prefill)
      } else {
        applyPrefill(prefill)
      }
    }
    // Only re-run when the dialog opens / prefill identity changes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, prefill])

  useEffect(() => {
    if (!aliasMenuOpen) return
    function onDown(e: MouseEvent) {
      if (aliasMenuRef.current && !aliasMenuRef.current.contains(e.target as Node)) {
        setAliasMenuOpen(false)
      }
    }
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') setAliasMenuOpen(false)
    }
    document.addEventListener('mousedown', onDown)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('mousedown', onDown)
      document.removeEventListener('keydown', onKey)
    }
  }, [aliasMenuOpen])

  if (!open) return null

  function resetForm() {
    setStep('method')
    setMethod('ssh')
    setHost('')
    setPort(22)
    setUser('root')
    setAuthMethod('key')
    setPassword('')
    setKeyPath('~/.ssh/id_rsa')
    setPassphrase('')
    setAliases([])
    setSelectedAlias('')
    setAliasMenuOpen(false)
    setContainers([])
    setConnectionId('')
    setCurrentDir('')
    setDirs([])
    setAliasName('')
    setError('')
    setHostKeyPrompt(null)
    hostKeyRetryRef.current = null
    setBinding(false)
  }

  function applyPrefill(p: RemotePrefill) {
    setHost(p.host)
    setPort(p.port || 22)
    setUser(p.user || 'root')
    setMethod(p.kind === 'docker' ? 'docker' : 'ssh')
    setStep(p.kind === 'docker' ? 'docker' : 'config')
  }

  async function loadAliases() {
    try {
      const res = await api.sshList()
      setAliases(res.aliases || [])
    } catch {
      setAliases([])
    }
  }

  async function loadContainers() {
    setContainersLoading(true)
    setError('')
    try {
      const res = await api.dockerContainers()
      setContainers(
        (res.containers || []).slice().sort((a, b) => {
          if (a.running !== b.running) return a.running ? -1 : 1
          return (a.name || a.id).localeCompare(b.name || b.id)
        }),
      )
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to list containers')
      setContainers([])
    } finally {
      setContainersLoading(false)
    }
  }

  function applyAlias(name: string) {
    setSelectedAlias(name)
    setAliasMenuOpen(false)
    const alias = aliases.find((a) => a.name === name)
    if (!alias) return
    const at = alias.addr.indexOf('@')
    const nextUser = at >= 0 ? alias.addr.slice(0, at) : ''
    let nextHost = at >= 0 ? alias.addr.slice(at + 1) : alias.addr
    const colon = nextHost.lastIndexOf(':')
    if (colon >= 0) {
      setPort(parseInt(nextHost.slice(colon + 1), 10) || 22)
      nextHost = nextHost.slice(0, colon)
    }
    if (nextUser) setUser(nextUser)
    setHost(nextHost)
    if (alias.path) setAliasName(alias.name)
  }

  async function discardConnection(id?: string) {
    const cid = id ?? connectionId
    if (!cid || boundRef.current) {
      if (!boundRef.current) setConnectionId('')
      return
    }
    try {
      await api.remoteCancel(cid)
    } catch {
      // best-effort
    }
    setConnectionId('')
  }

  /** Seamless SSH reconnect with key/agent; fall back to prefilled form. */
  async function autoReconnectSSH(p: RemotePrefill, confirmedFingerprint?: string) {
    applyPrefill(p)
    setMethod('ssh')
    setStep('connecting')
    try {
      const res = await api.remoteConnect(sshReconnectRequest(p, confirmedFingerprint))
      setConnectionId(res.connection_id)
      const dir = p.remotePath && p.remotePath !== '/' ? p.remotePath : res.remote_pwd
      setCurrentDir(dir)
      const ok = await bindWith(res.connection_id, dir)
      if (!ok && !boundRef.current) {
        if (res.connection_id) {
          await listDir(res.connection_id, dir)
          setStep('dir')
        } else {
          setStep('config')
        }
      }
    } catch (e) {
      if (showHostKeyPrompt(e, (fingerprint) => autoReconnectSSH(p, fingerprint))) {
        setStep('config')
        return
      }
      setError('')
      setStep('config')
    }
  }

  async function autoReconnectDocker(p: RemotePrefill) {
    setMethod('docker')
    if (!p.container) {
      setStep('docker')
      void loadContainers()
      return
    }
    setStep('connecting')
    try {
      const res = await api.remoteConnect({ type: 'docker', container: p.container })
      setConnectionId(res.connection_id)
      const dir = p.remotePath && p.remotePath !== '/' ? p.remotePath : res.remote_pwd
      setCurrentDir(dir)
      const ok = await bindWith(res.connection_id, dir)
      if (!ok && !boundRef.current && res.connection_id) {
        await listDir(res.connection_id, dir)
        setStep('dir')
      }
    } catch {
      setError('')
      setStep('docker')
      void loadContainers()
    }
  }

  async function connectSSH(confirmedFingerprint?: string) {
    if (!host.trim()) {
      setError('Host is required')
      return
    }
    await discardConnection()
    setError('')
    setHostKeyPrompt(null)
    setStep('connecting')
    try {
      const res = await api.remoteConnect({
        type: 'ssh',
        host: host.trim(),
        port,
        user: user.trim() || 'root',
        auth_method: authMethod,
        password: authMethod === 'password' ? password : undefined,
        key_path: authMethod === 'key' ? keyPath.trim() : undefined,
        passphrase: authMethod === 'key' ? passphrase : undefined,
        accept_host_key: confirmedFingerprint ? true : undefined,
        host_key_fingerprint: confirmedFingerprint,
      })
      setConnectionId(res.connection_id)
      await listDir(res.connection_id, res.remote_pwd)
      setStep('dir')
    } catch (e) {
      if (showHostKeyPrompt(e, (fingerprint) => connectSSH(fingerprint))) {
        setStep('config')
        return
      }
      setError(e instanceof Error ? e.message : 'Connection failed')
      setStep('config')
    }
  }

  function showHostKeyPrompt(
    errorValue: unknown,
    retry: (fingerprint?: string) => Promise<void>,
  ): boolean {
    if (!isAPIError(errorValue) || errorValue.status !== 409 || !errorValue.body || typeof errorValue.body !== 'object') return false
    const body = errorValue.body as Partial<RemoteHostKeyErrorPayload>
    if (
      body.code !== 'ssh_host_key_unknown' &&
      body.code !== 'ssh_host_key_changed' &&
      body.code !== 'ssh_host_key_confirmation_mismatch'
    ) return false
    if (!body.host || !body.fingerprint || !body.key_type) return false
    setHostKeyPrompt({
      error: body.error || errorValue.message,
      code: body.code,
      host: body.host,
      fingerprint: body.fingerprint,
      key_type: body.key_type,
      old_fingerprint: body.old_fingerprint,
      expected_fingerprint: body.expected_fingerprint,
    })
    hostKeyRetryRef.current = retry
    setError('')
    return true
  }

  async function connectDocker(container: string) {
    if (!container) return
    await discardConnection()
    setError('')
    setStep('connecting')
    try {
      const res = await api.remoteConnect({ type: 'docker', container })
      setConnectionId(res.connection_id)
      await listDir(res.connection_id, res.remote_pwd)
      setStep('dir')
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Connection failed')
      setStep('docker')
    }
  }

  async function listDir(connId: string, path: string) {
    setDirLoading(true)
    setError('')
    try {
      const res = await api.remoteListDir(connId, path)
      setCurrentDir(res.path)
      setDirs(res.dirs || [])
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to list directory')
    } finally {
      setDirLoading(false)
    }
  }

  function navigate(name: string) {
    if (!connectionId) return
    if (name === '..') {
      const parent = currentDir.split('/').slice(0, -1).join('/') || '/'
      void listDir(connectionId, parent)
      return
    }
    const base = currentDir.endsWith('/') ? currentDir : `${currentDir}/`
    void listDir(connectionId, `${base}${name}`)
  }

  async function bindWith(connId: string, dir: string): Promise<boolean> {
    if (!connId || binding) return false
    setBinding(true)
    setError('')
    try {
      const res = await api.remoteBind(connId, dir, { focus: true })
      if (res.kind === 'docker') {
        const name = aliasName.trim() || res.container || 'container'
        await api.remoteSaveDockerAlias(name, res.container || '', res.remote_path).catch(() => {})
      } else {
        const addr = `${res.user}@${res.host}`
        await api.remoteSaveAlias(aliasName.trim() || addr, addr, res.remote_path).catch(() => {})
      }
      boundRef.current = true
      setConnectionId('')
      dispatch(chatActions.clearChat())
      dispatch(sessionActions.setProjectPath(res.project || res.workspace_key || res.label || res.pwd))
      dispatch(sessionActions.setCurrentSession(''))
      await dispatch(loadWorkspaceState())
      onBound?.()
      onClose()
      return true
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to bind workspace')
      return false
    } finally {
      setBinding(false)
    }
  }

  async function bindHere() {
    if (!connectionId) return
    await bindWith(connectionId, currentDir)
  }

  function close() {
    void discardConnection()
    onClose()
  }

  function goBack() {
    if (step === 'method') {
      close()
      return
    }
    if (step === 'dir') {
      void discardConnection()
      setStep(method === 'docker' ? 'docker' : 'config')
      return
    }
    if (step === 'config' || step === 'docker' || step === 'connecting') {
      void discardConnection()
      setStep('method')
      setError('')
    }
  }

  const connecting = step === 'connecting'
  const showSSHForm = step === 'config' || (connecting && method !== 'docker')
  const showDocker = step === 'docker' || (connecting && method === 'docker')

  function proceedFromMethod() {
    if (method === 'docker') {
      setStep('docker')
      void loadContainers()
    } else setStep('config')
  }

  function backToConfig() {
    void discardConnection()
    setStep(method === 'docker' ? 'docker' : 'config')
  }

  return (
    <div className="fixed inset-0 z-[var(--z-modal)]" role="dialog" aria-modal="true">
      <div className="fixed inset-0" style={{ background: 'var(--backdrop)', backdropFilter: 'blur(6px)' }} onClick={close} />
      <div className="titlebar-drag" data-tauri-drag-region aria-hidden="true" />
      <div className="fixed inset-0 flex items-center justify-center p-4">
        <div className="rcw">
          {/* Step rail — Vue .rcw-rail */}
          <div className="rcw-rail">
            <div className="rcw-rail-title">{t('wizard.title')}</div>
            <ol className="rcw-steps">
              {steps.map((s, i) => (
                <li key={s.key} className={`rcw-step${i < stepIndex ? ' done' : ''}${i === stepIndex ? ' current' : ''}`}>
                  <span className="rcw-step-dot">
                    {i < stepIndex ? <CheckIcon className="h-3 w-3" /> : i + 1}
                  </span>
                  <span className="rcw-step-label">{s.label}</span>
                </li>
              ))}
            </ol>
          </div>

          <div className="rcw-body">
            <button type="button" className="rcw-close" onClick={close} aria-label={t('common.close')}>
              <XMarkIcon className="h-4 w-4" />
            </button>

            {/* Method */}
            {step === 'method' && (
              <>
                <h3 className="rcw-h">{t('wizard.chooseMethod')}</h3>
                <p className="rcw-sub">{t('wizard.pickMethodDesc')}</p>
                <div className="rcw-methods">
                  <button type="button" className={`rcw-method${method === 'ssh' ? ' active' : ''}`} onClick={() => setMethod('ssh')}>
                    <ServerIcon className="h-5 w-5" />
                    <span className="rcw-method-name">{t('wizard.ssh')}</span>
                    <span className="rcw-method-desc">{t('wizard.remoteHost')}</span>
                  </button>
                  <button type="button" className={`rcw-method${method === 'docker' ? ' active' : ''}`} onClick={() => setMethod('docker')}>
                    <CubeIcon className="h-5 w-5" />
                    <span className="rcw-method-name">{t('wizard.docker')}</span>
                    <span className="rcw-method-desc">{t('wizard.container')}</span>
                  </button>
                </div>
                <div className="rcw-foot">
                  <span />
                  <button type="button" className="rcw-primary" onClick={proceedFromMethod}>
                    {t('wizard.next')} <ChevronRightIcon className="h-3.5 w-3.5" />
                  </button>
                </div>
              </>
            )}

            {/* SSH config (kept visible under connecting overlay) */}
            {showSSHForm && (
              <>
                <h3 className="rcw-h">{t('wizard.sshConnection')}</h3>
                <p className="rcw-sub">{t('wizard.sshDesc')}</p>
                {error && <div className="rcw-error">{error}</div>}
                {hostKeyPrompt && (
                  <div className={`rcw-host-key${hostKeyPrompt.code === 'ssh_host_key_changed' ? ' is-danger' : ''}`}>
                    <div className="rcw-host-key-title">
                      {hostKeyPrompt.code === 'ssh_host_key_unknown'
                        ? t('conversationLoading.hostKey.unknownTitle')
                        : hostKeyPrompt.code === 'ssh_host_key_changed'
                          ? t('conversationLoading.hostKey.changedTitle')
                          : t('conversationLoading.hostKey.mismatchTitle')}
                    </div>
                    <div className="rcw-host-key-body">
                      {hostKeyPrompt.code === 'ssh_host_key_unknown'
                        ? t('conversationLoading.hostKey.unknownBody')
                        : hostKeyPrompt.code === 'ssh_host_key_changed'
                          ? t('conversationLoading.hostKey.changedBody')
                          : t('conversationLoading.hostKey.mismatchBody')}
                    </div>
                    <div className="rcw-host-key-fingerprint">{hostKeyPrompt.fingerprint}</div>
                    {hostKeyPrompt.old_fingerprint && <div className="rcw-host-key-old">{hostKeyPrompt.old_fingerprint}</div>}
                    {hostKeyPrompt.expected_fingerprint && (
                      <div className="rcw-host-key-old">
                        {t('conversationLoading.hostKey.expected')}: {hostKeyPrompt.expected_fingerprint}
                      </div>
                    )}
                    <div className="rcw-host-key-actions">
                      <button type="button" className="rcw-ghost" onClick={() => setHostKeyPrompt(null)}>{t('common.cancel')}</button>
                      {hostKeyPrompt.code === 'ssh_host_key_unknown' && (
                        <button type="button" className="rcw-primary" onClick={() => void hostKeyRetryRef.current?.(hostKeyPrompt.fingerprint)}>
                          {t('conversationLoading.hostKey.accept')}
                        </button>
                      )}
                      {hostKeyPrompt.code === 'ssh_host_key_confirmation_mismatch' && (
                        <button type="button" className="rcw-primary" onClick={() => void hostKeyRetryRef.current?.()}>{t('common.retry')}</button>
                      )}
                    </div>
                  </div>
                )}
                {connecting && (
                  <div className="rcw-hint">
                    <ArrowPathIcon className="h-3.5 w-3.5 spin" /> {t('wizard.connecting')}
                  </div>
                )}
                <div className={connecting ? 'rcw-disabled' : undefined}>
                  {aliases.length > 0 && (
                    <div className="rcw-field">
                      <label>{t('settings.ssh.savedAlias')}</label>
                      {/*
                        Custom listbox (not native <select>): Tauri/WebView native
                        dropdowns ignore app tokens and break theming. Same pattern
                        as ModelSelector / WorkspacePicker — no jcode-ui-core Select.
                      */}
                      <div className="rcw-select" ref={aliasMenuRef}>
                        <button
                          type="button"
                          className="rcw-select-trigger"
                          disabled={connecting}
                          aria-haspopup="listbox"
                          aria-expanded={aliasMenuOpen}
                          onClick={() => setAliasMenuOpen((v) => !v)}
                        >
                          <span className="rcw-select-value">{selectedAliasLabel}</span>
                          <ChevronDownIcon className={`h-3.5 w-3.5 rcw-select-chev${aliasMenuOpen ? ' open' : ''}`} />
                        </button>
                        {aliasMenuOpen && !connecting && (
                          <div className="rcw-select-menu" role="listbox">
                            <button
                              type="button"
                              role="option"
                              aria-selected={!selectedAlias}
                              className={`rcw-select-option${!selectedAlias ? ' is-selected' : ''}`}
                              onClick={() => applyAlias('')}
                            >
                              <span>{t('settings.ssh.noAlias')}</span>
                              {!selectedAlias && <CheckIcon className="h-3.5 w-3.5" />}
                            </button>
                            {aliases.map((a) => {
                              const selected = selectedAlias === a.name
                              return (
                                <button
                                  key={a.name}
                                  type="button"
                                  role="option"
                                  aria-selected={selected}
                                  className={`rcw-select-option${selected ? ' is-selected' : ''}`}
                                  onClick={() => applyAlias(a.name)}
                                >
                                  <span className="rcw-select-option-main">
                                    <span className="rcw-select-option-label">{a.name}</span>
                                    <span className="rcw-select-option-desc">{a.addr}</span>
                                  </span>
                                  {selected && <CheckIcon className="h-3.5 w-3.5" />}
                                </button>
                              )
                            })}
                          </div>
                        )}
                      </div>
                    </div>
                  )}
                  <div className="rcw-row">
                    <div className="rcw-field grow">
                      <label>{t('wizard.host')}</label>
                      <input className="rcw-input" value={host} disabled={connecting} onChange={(e) => setHost(e.target.value)} placeholder={t('wizard.hostPlaceholder')} />
                    </div>
                    <div className="rcw-field port">
                      <label>{t('wizard.port')}</label>
                      <input className="rcw-input" type="number" value={port} disabled={connecting} onChange={(e) => setPort(Number(e.target.value) || 22)} />
                    </div>
                  </div>
                  <div className="rcw-row">
                    <div className="rcw-field grow">
                      <label>{t('wizard.user')}</label>
                      <input className="rcw-input" value={user} disabled={connecting} onChange={(e) => setUser(e.target.value)} placeholder={t('wizard.userPlaceholder')} />
                    </div>
                    <div className="rcw-field">
                      <label>{t('wizard.auth')}</label>
                      <div className="rcw-seg">
                        <button type="button" className={authMethod === 'password' ? 'on' : ''} disabled={connecting} onClick={() => setAuthMethod('password')}>{t('wizard.authPassword')}</button>
                        <button type="button" className={authMethod === 'key' ? 'on' : ''} disabled={connecting} onClick={() => setAuthMethod('key')}>{t('wizard.authKey')}</button>
                      </div>
                    </div>
                  </div>
                  {authMethod === 'password' ? (
                    <div className="rcw-field">
                      <label>{t('wizard.password')}</label>
                      <input className="rcw-input" type="password" value={password} disabled={connecting} onChange={(e) => setPassword(e.target.value)} />
                    </div>
                  ) : (
                    <>
                      <div className="rcw-field">
                        <label>{t('wizard.keyPath')}</label>
                        <input className="rcw-input mono" value={keyPath} disabled={connecting} onChange={(e) => setKeyPath(e.target.value)} placeholder="~/.ssh/id_rsa" />
                      </div>
                      <div className="rcw-field">
                        <label>{t('wizard.passphrase')}</label>
                        <input className="rcw-input" type="password" value={passphrase} disabled={connecting} onChange={(e) => setPassphrase(e.target.value)} />
                      </div>
                    </>
                  )}
                </div>
                <div className="rcw-foot">
                  <button type="button" className="rcw-ghost" disabled={connecting} onClick={goBack}>{t('common.back')}</button>
                  <button type="button" className="rcw-primary" disabled={connecting} onClick={() => void connectSSH()}>
                    {connecting ? <><ArrowPathIcon className="h-3.5 w-3.5 spin" /> {t('wizard.connecting')}</> : t('wizard.connect')}
                  </button>
                </div>
              </>
            )}

            {/* Docker */}
            {showDocker && (
              <>
                <h3 className="rcw-h">{t('wizard.dockerConnection')}</h3>
                <p className="rcw-sub">{t('wizard.dockerDesc')}</p>
                {error && <div className="rcw-error">{error}</div>}
                {connecting ? (
                  <div className="rcw-hint">
                    <ArrowPathIcon className="h-3.5 w-3.5 spin" /> {t('wizard.connecting')}
                  </div>
                ) : (
                  <>
                    <div className="rcw-dirbar">
                      <span className="rcw-dir-path">{t('wizard.selectContainer')}</span>
                      <button type="button" className="rcw-back" style={{ marginLeft: 'auto' }} title={t('wizard.refresh')} onClick={() => void loadContainers()}>
                        <ArrowPathIcon className={`h-3.5 w-3.5${containersLoading ? ' spin' : ''}`} />
                      </button>
                    </div>
                    <div className="rcw-dirlist">
                      {containersLoading ? (
                        <div className="rcw-hint"><ArrowPathIcon className="h-3.5 w-3.5 spin" /> {t('wizard.loading')}</div>
                      ) : containers.length === 0 ? (
                        <div className="rcw-hint">{t('wizard.noContainers')}</div>
                      ) : (
                        containers.map((c) => (
                          <button key={c.id} type="button" className="rcw-dir-row" onClick={() => void connectDocker(c.name || c.id)}>
                            <CubeIcon className="h-3.5 w-3.5" />
                            <span className="rcw-ctr-name">{c.name || c.id.slice(0, 12)}</span>
                            <span className="rcw-ctr-img">{c.image}</span>
                            <span className={`rcw-ctr-state${c.running ? ' on' : ''}`}>{c.running ? t('wizard.running') : t('wizard.stopped')}</span>
                          </button>
                        ))
                      )}
                    </div>
                    <div className="rcw-foot">
                      <button type="button" className="rcw-ghost" onClick={goBack}>{t('common.back')}</button>
                      <span />
                    </div>
                  </>
                )}
              </>
            )}

            {/* Directory */}
            {step === 'dir' && (
              <>
                <h3 className="rcw-h">{t('wizard.selectDirectory')}</h3>
                <p className="rcw-sub">{t('wizard.dirDesc')}</p>
                {error && <div className="rcw-error">{error}</div>}
                <div className="rcw-dirbar">
                  <button type="button" className="rcw-back" title={t('wizard.backToConfig')} onClick={backToConfig}>
                    <ArrowLeftIcon className="h-3.5 w-3.5" />
                  </button>
                  <span className="rcw-dir-path">{currentDir || '/'}</span>
                </div>
                <div className="rcw-dirlist">
                  {dirLoading ? (
                    <div className="rcw-hint"><ArrowPathIcon className="h-3.5 w-3.5 spin" /> {t('wizard.loading')}</div>
                  ) : (
                    <>
                      {currentDir !== '/' && (
                        <button type="button" className="rcw-dir-row" onClick={() => navigate('..')}>
                          <span className="rcw-up">..</span>
                          <span>..</span>
                        </button>
                      )}
                      {dirs.map((d) => (
                        <button key={d} type="button" className="rcw-dir-row" onClick={() => navigate(d)}>
                          <FolderIcon className="h-3.5 w-3.5" />
                          <span>{d}</span>
                        </button>
                      ))}
                      {dirs.length === 0 && <div className="rcw-hint">{t('wizard.noSubDirs')}</div>}
                    </>
                  )}
                </div>
                <label className="rcw-save">
                  <span>{t('wizard.saveAlias')}</span>
                  <input className="rcw-input mini" value={aliasName} onChange={(e) => setAliasName(e.target.value)} placeholder={t('wizard.aliasNamePlaceholder')} />
                </label>
                <div className="rcw-foot">
                  <button type="button" className="rcw-ghost" disabled={binding} onClick={close}>{t('common.cancel')}</button>
                  <button type="button" className="rcw-primary" disabled={binding} onClick={() => void bindHere()}>
                    {binding ? <ArrowPathIcon className="h-3.5 w-3.5 spin" /> : null}
                    {t('wizard.useDirectory')}
                  </button>
                </div>
              </>
            )}
          </div>
        </div>
      </div>

      <style>{RCW_CSS}</style>
    </div>
  )
}

/** Ported from web/src/components/RemoteConnectWizard.vue scoped styles. */
const RCW_CSS = `
.rcw {
  display: flex;
  width: 900px;
  max-width: calc(100vw - 32px);
  height: 600px;
  max-height: 86vh;
  overflow: hidden;
  background: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-xl, 16px);
  box-shadow: var(--shadow-lg);
  position: relative;
  z-index: 1;
}
.rcw-rail {
  width: 220px;
  flex-shrink: 0;
  padding: 22px 18px;
  background: var(--color-background);
  border-right: 1px solid var(--color-border);
}
.rcw-rail-title {
  font-size: 12px;
  font-weight: 600;
  color: var(--color-muted-foreground);
  margin-bottom: 22px;
}
.rcw-steps {
  display: flex;
  flex-direction: column;
  gap: 6px;
  list-style: none;
  padding: 0;
  margin: 0;
}
.rcw-step {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 9px 10px;
  border-radius: var(--radius-lg);
  color: var(--color-muted-foreground);
}
.rcw-step.current {
  background: var(--color-surface);
  color: var(--color-foreground);
}
.rcw-step-dot {
  display: grid;
  place-items: center;
  width: 24px;
  height: 24px;
  flex-shrink: 0;
  border-radius: 50%;
  border: 1px solid var(--color-border);
  font-size: 12px;
  font-weight: 600;
}
.rcw-step.current .rcw-step-dot {
  background: var(--color-foreground);
  color: var(--color-background);
  border-color: var(--color-foreground);
}
.rcw-step.done .rcw-step-dot {
  background: var(--color-success, #16a34a);
  border-color: var(--color-success, #16a34a);
  color: var(--color-surface);
}
.rcw-step-label { font-size: 13px; }
.rcw-body {
  flex: 1;
  min-width: 0;
  position: relative;
  padding: 24px 26px;
  overflow-y: auto;
}
.rcw-close {
  position: absolute;
  top: 18px;
  right: 18px;
  border: none;
  background: transparent;
  color: var(--color-muted-foreground);
  cursor: pointer;
  display: grid;
  place-items: center;
}
.rcw-close:hover { color: var(--color-foreground); }
.rcw-h {
  font-size: 18px;
  font-weight: 600;
  color: var(--color-foreground);
  padding-right: 28px;
}
.rcw-sub {
  margin-top: 6px;
  margin-bottom: 18px;
  font-size: 12.5px;
  color: var(--color-muted-foreground);
}
.rcw-error {
  margin-bottom: 14px;
  padding: 8px 12px;
  font-size: 12px;
  border-radius: var(--radius-lg);
  color: var(--color-error-fg, #b91c1c);
  background: var(--color-error-bg, rgba(220, 38, 38, 0.08));
  border: 1px solid var(--color-error-fg, rgba(220, 38, 38, 0.3));
  word-break: break-word;
}
.rcw-host-key {
  margin-bottom: 14px;
  padding: 12px;
  border: 1px solid var(--color-warning-fg);
  border-radius: var(--radius-lg);
  background: var(--color-warning-bg);
}
.rcw-host-key.is-danger {
  border-color: var(--color-error-fg);
  background: var(--color-error-bg);
}
.rcw-host-key-title {
  color: var(--color-foreground);
  font-size: 12px;
  font-weight: 650;
}
.rcw-host-key-body {
  margin-top: 4px;
  color: var(--color-muted-foreground);
  font-size: 11px;
  line-height: 1.45;
}
.rcw-host-key-fingerprint,
.rcw-host-key-old {
  margin-top: 8px;
  overflow-wrap: anywhere;
  color: var(--color-foreground);
  font-family: var(--font-mono);
  font-size: 10.5px;
}
.rcw-host-key-old {
  color: var(--color-muted-foreground);
  text-decoration: line-through;
}
.rcw-host-key-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  margin-top: 10px;
}
.rcw-methods {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 14px;
}
.rcw-method {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 10px;
  padding: 20px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-xl);
  background: var(--color-background);
  cursor: pointer;
  color: var(--color-foreground);
  transition: border-color 0.15s, background 0.15s;
  text-align: left;
}
.rcw-method:hover { border-color: color-mix(in srgb, var(--color-foreground) 30%, transparent); }
.rcw-method.active {
  border-color: var(--color-foreground);
  background: var(--color-surface);
}
.rcw-method-name { font-size: 15px; font-weight: 600; }
.rcw-method-desc { font-size: 12px; color: var(--color-muted-foreground); }
.rcw-row { display: flex; gap: 12px; }
.rcw-field {
  display: flex;
  flex-direction: column;
  gap: 5px;
  margin-bottom: 14px;
}
.rcw-field.grow { flex: 1; min-width: 0; }
.rcw-field.port { width: 96px; flex-shrink: 0; }
.rcw-field label { font-size: 12px; color: var(--color-muted-foreground); }
.rcw-input {
  width: 100%;
  padding: 9px 11px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-lg);
  background: var(--color-background);
  color: var(--color-foreground);
  font-size: 13px;
  outline: none;
  transition: border-color 0.15s;
}
.rcw-input:focus { border-color: var(--color-accent-neutral); }
.rcw-input.mono { font-family: var(--font-mono); font-size: 12px; }
.rcw-input.mini { width: 120px; padding: 5px 9px; font-size: 12px; }
.rcw-input:disabled { opacity: 0.55; }
.rcw-select { position: relative; }
.rcw-select-trigger {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  padding: 9px 11px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-lg);
  background: var(--color-background);
  color: var(--color-foreground);
  font-size: 13px;
  text-align: left;
  cursor: pointer;
  outline: none;
  transition: border-color 0.15s;
}
.rcw-select-trigger:hover:not(:disabled) { border-color: color-mix(in srgb, var(--color-foreground) 30%, transparent); }
.rcw-select-trigger:focus-visible { border-color: var(--color-accent-neutral); }
.rcw-select-trigger:disabled { opacity: 0.55; cursor: not-allowed; }
.rcw-select-value {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.rcw-select-chev {
  flex-shrink: 0;
  color: var(--color-muted-foreground);
  transition: transform 0.15s;
}
.rcw-select-chev.open { transform: rotate(180deg); }
.rcw-select-menu {
  position: absolute;
  z-index: 20;
  top: calc(100% + 4px);
  left: 0;
  right: 0;
  max-height: 220px;
  overflow-y: auto;
  padding: 4px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-lg);
  background: var(--color-surface);
  box-shadow: var(--shadow-lg);
}
.rcw-select-option {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  padding: 8px 10px;
  border: none;
  border-radius: var(--radius-md);
  background: transparent;
  color: var(--color-foreground);
  font-size: 13px;
  text-align: left;
  cursor: pointer;
}
.rcw-select-option:hover { background: var(--color-muted); }
.rcw-select-option.is-selected { background: color-mix(in srgb, var(--color-foreground) 6%, transparent); }
.rcw-select-option svg { flex-shrink: 0; color: var(--color-muted-foreground); }
.rcw-select-option-main {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 1px;
}
.rcw-select-option-label {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-weight: 500;
}
.rcw-select-option-desc {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-family: var(--font-mono);
  font-size: 11px;
  color: var(--color-muted-foreground);
}
.rcw-seg {
  display: flex;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-lg);
  overflow: hidden;
}
.rcw-seg button {
  padding: 8px 14px;
  border: none;
  background: var(--color-background);
  color: var(--color-muted-foreground);
  font-size: 12.5px;
  cursor: pointer;
}
.rcw-seg button.on {
  background: var(--color-foreground);
  color: var(--color-background);
}
.rcw-seg button:disabled { opacity: 0.55; cursor: not-allowed; }
.rcw-dirbar {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 10px;
  margin-bottom: 8px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-lg);
  background: var(--color-background);
}
.rcw-back {
  display: grid;
  place-items: center;
  border: none;
  background: transparent;
  color: var(--color-muted-foreground);
  cursor: pointer;
  flex-shrink: 0;
}
.rcw-back:hover { color: var(--color-foreground); }
.rcw-dir-path {
  font-family: var(--font-mono);
  font-size: 12px;
  color: var(--color-foreground);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.rcw-dirlist {
  height: 190px;
  overflow-y: auto;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-lg);
  background: var(--color-background);
  padding: 4px;
}
.rcw-dir-row {
  display: flex;
  align-items: center;
  gap: 9px;
  width: 100%;
  padding: 8px 10px;
  border: none;
  background: transparent;
  border-radius: var(--radius-md);
  color: var(--color-foreground);
  font-size: 13px;
  cursor: pointer;
  text-align: left;
}
.rcw-dir-row:hover { background: var(--color-muted); }
.rcw-dir-row svg { color: var(--color-muted-foreground); flex-shrink: 0; }
.rcw-up { width: 14px; text-align: center; font-family: var(--font-mono); font-size: 12px; color: var(--color-muted-foreground); }
.rcw-ctr-name { font-weight: 500; flex-shrink: 0; }
.rcw-ctr-img {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 12px;
  color: var(--color-muted-foreground);
}
.rcw-ctr-state {
  flex-shrink: 0;
  font-size: 11px;
  padding: 2px 7px;
  border-radius: 999px;
  background: var(--color-muted);
  color: var(--color-muted-foreground);
}
.rcw-ctr-state.on {
  background: color-mix(in srgb, var(--color-success, #16a34a) 18%, transparent);
  color: var(--color-success, #16a34a);
}
.rcw-hint {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  padding: 24px;
  font-size: 12px;
  color: var(--color-muted-foreground);
}
.rcw-save {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 12px;
  font-size: 12.5px;
  color: var(--color-muted-foreground);
  cursor: pointer;
}
.rcw-foot {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-top: 22px;
}
.rcw-primary {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 9px 18px;
  border: none;
  border-radius: var(--radius-lg);
  background: var(--color-accent-neutral);
  color: var(--color-surface);
  font-size: 13px;
  font-weight: 500;
  cursor: pointer;
  transition: opacity 0.15s;
}
.rcw-primary:hover:not(:disabled) { opacity: 0.9; }
.rcw-primary:disabled { opacity: 0.6; cursor: not-allowed; }
.rcw-ghost {
  padding: 9px 16px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-lg);
  background: transparent;
  color: var(--color-foreground);
  font-size: 13px;
  cursor: pointer;
}
.rcw-ghost:disabled { opacity: 0.5; cursor: not-allowed; }
.rcw-disabled { opacity: 0.55; pointer-events: none; }
.spin { animation: rcw-spin 0.8s linear infinite; }
@keyframes rcw-spin { to { transform: rotate(360deg); } }
@media (max-width: 720px) {
  .rcw-rail { display: none; }
  .rcw { height: auto; max-height: 90vh; }
}
`
