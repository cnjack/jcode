import { useEffect, useState } from 'react'
import {
  ArrowLeftIcon,
  ArrowPathIcon,
  CheckIcon,
  CubeIcon,
  FolderIcon,
  ServerIcon,
  XMarkIcon,
} from '@heroicons/react/24/outline'
import { useTranslation } from 'react-i18next'
import { useAppDispatch } from '../app/hooks'
import { chatActions, loadWorkspaceState, sessionActions } from '../app/store'
import { api } from '../lib/api'
import type { DockerContainer, RemoteAuthMethod, RemoteKind, SSHAlias } from '../lib/types'

type Step = 'method' | 'config' | 'docker' | 'connecting' | 'dir'

export interface RemoteConnectWizardProps {
  open: boolean
  onClose: () => void
  onBound?: () => void
}

export function RemoteConnectWizard({ open, onClose, onBound }: RemoteConnectWizardProps) {
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
  const [containers, setContainers] = useState<DockerContainer[]>([])
  const [containersLoading, setContainersLoading] = useState(false)
  const [connectionId, setConnectionId] = useState('')
  const [currentDir, setCurrentDir] = useState('')
  const [dirs, setDirs] = useState<string[]>([])
  const [dirLoading, setDirLoading] = useState(false)
  const [aliasName, setAliasName] = useState('')
  const [error, setError] = useState('')
  const [binding, setBinding] = useState(false)

  useEffect(() => {
    if (!open) return
    reset()
    void loadAliases()
  }, [open])

  if (!open) return null

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
      setContainers((res.containers || []).slice().sort((a, b) => {
        if (a.running !== b.running) return a.running ? -1 : 1
        return (a.name || a.id).localeCompare(b.name || b.id)
      }))
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to list containers')
      setContainers([])
    } finally {
      setContainersLoading(false)
    }
  }

  function applyAlias(alias: SSHAlias) {
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

  async function discardConnection() {
    if (!connectionId) return
    try {
      await api.remoteCancel(connectionId)
    } catch {
      // best-effort cleanup
    }
    setConnectionId('')
  }

  async function connectSSH() {
    if (!host.trim()) {
      setError('Host is required')
      return
    }
    await discardConnection()
    setError('')
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
      })
      setConnectionId(res.connection_id)
      await listDir(res.connection_id, res.remote_pwd)
      setStep('dir')
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Connection failed')
      setStep('config')
    }
  }

  async function connectDocker(container: string) {
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

  async function bindHere() {
    if (!connectionId || binding) return
    setBinding(true)
    setError('')
    try {
      const res = await api.remoteBind(connectionId, currentDir)
      if (res.kind === 'docker') {
        const name = aliasName.trim() || res.container || 'container'
        await api.remoteSaveDockerAlias(name, res.container || '', res.remote_path).catch(() => {})
      } else {
        const addr = `${res.user}@${res.host}`
        await api.remoteSaveAlias(aliasName.trim() || addr, addr, res.remote_path).catch(() => {})
      }
      setConnectionId('')
      dispatch(chatActions.clearChat())
      dispatch(sessionActions.setProjectPath(res.label || res.pwd))
      dispatch(sessionActions.setCurrentSession(''))
      await dispatch(loadWorkspaceState())
      onBound?.()
      close()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to bind workspace')
    } finally {
      setBinding(false)
    }
  }

  function reset() {
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
    setContainers([])
    setConnectionId('')
    setCurrentDir('')
    setDirs([])
    setAliasName('')
    setError('')
    setBinding(false)
  }

  function close() {
    void discardConnection()
    onClose()
  }

  return (
    <div className="fixed inset-0 z-[var(--z-modal)] flex items-center justify-center p-4" role="dialog" aria-modal="true">
      <div className="absolute inset-0 bg-[var(--backdrop)] backdrop-blur-[6px]" onClick={close} />
      <div className="titlebar-drag" data-tauri-drag-region aria-hidden="true" />
      <div className="relative flex max-h-[88vh] w-full max-w-[640px] flex-col overflow-hidden rounded-[var(--radius-2xl)] border border-[var(--color-border)] bg-[var(--color-surface)] shadow-[var(--shadow-lg)]">
        <header className="flex items-center justify-between border-b border-[var(--color-border)] px-5 py-4">
          <div>
            <h2 className="text-base font-semibold text-[var(--color-foreground)]">{t('nav.remoteConnect')}</h2>
            <p className="mt-1 text-xs text-[var(--color-muted-foreground)]">{t('wizard.pickMethodDesc')}</p>
          </div>
          <button type="button" onClick={close} aria-label={t('common.close')} className="grid h-8 w-8 place-items-center rounded-[var(--radius-md)] text-[var(--color-muted-foreground)] hover:bg-[var(--color-muted)]">
            <XMarkIcon className="h-4 w-4" />
          </button>
        </header>

        <div className="min-h-0 flex-1 overflow-y-auto p-5">
          {step === 'method' && (
            <div className="grid grid-cols-2 gap-3">
              <MethodCard active={method === 'ssh'} Icon={ServerIcon} title="SSH" desc={t('wizard.sshDesc')} onClick={() => setMethod('ssh')} />
              <MethodCard active={method === 'docker'} Icon={CubeIcon} title="Docker" desc={t('wizard.dockerDesc')} onClick={() => setMethod('docker')} />
            </div>
          )}

          {step === 'config' && (
            <div className="space-y-3">
              {aliases.length > 0 && (
                <label className="block">
                  <span className="mb-1 block text-xs font-medium text-[var(--color-muted-foreground)]">{t('settings.ssh.aliases')}</span>
                  <select onChange={(e) => {
                    const alias = aliases.find((a) => a.name === e.target.value)
                    if (alias) applyAlias(alias)
                  }} className="w-full rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-background)] px-2 py-2 text-sm text-[var(--color-foreground)]">
                    <option value="">{t('settings.ssh.noAlias')}</option>
                    {aliases.map((alias) => <option key={alias.name} value={alias.name}>{alias.name} - {alias.addr}</option>)}
                  </select>
                </label>
              )}
              <div className="grid grid-cols-[1fr_90px] gap-2">
                <Field label="Host">
                  <input value={host} onChange={(e) => setHost(e.target.value)} placeholder={t('wizard.hostPlaceholder')} className={INPUT} />
                </Field>
                <Field label="Port">
                  <input value={port} onChange={(e) => setPort(Number(e.target.value) || 22)} type="number" className={INPUT} />
                </Field>
              </div>
              <Field label="User">
                <input value={user} onChange={(e) => setUser(e.target.value)} placeholder={t('wizard.userPlaceholder')} className={INPUT} />
              </Field>
              <Field label={t('wizard.auth')}>
                <select value={authMethod} onChange={(e) => setAuthMethod(e.target.value as RemoteAuthMethod)} className={INPUT}>
                  <option value="key">Key / agent</option>
                  <option value="password">Password</option>
                </select>
              </Field>
              {authMethod === 'password' ? (
                <Field label="Password">
                  <input value={password} onChange={(e) => setPassword(e.target.value)} type="password" className={INPUT} />
                </Field>
              ) : (
                <div className="grid grid-cols-2 gap-2">
                  <Field label="Key path">
                    <input value={keyPath} onChange={(e) => setKeyPath(e.target.value)} className={INPUT} />
                  </Field>
                  <Field label="Passphrase">
                    <input value={passphrase} onChange={(e) => setPassphrase(e.target.value)} type="password" className={INPUT} />
                  </Field>
                </div>
              )}
            </div>
          )}

          {step === 'docker' && (
            <div>
              <div className="mb-3 flex items-center justify-between">
                <h3 className="text-sm font-semibold text-[var(--color-foreground)]">{t('wizard.selectContainer')}</h3>
                <button type="button" onClick={() => void loadContainers()} className="inline-flex items-center gap-1.5 rounded-[var(--radius-md)] border border-[var(--color-border)] px-2 py-1 text-xs text-[var(--color-foreground)] hover:bg-[var(--color-muted)]">
                  <ArrowPathIcon className="h-3.5 w-3.5" />
                  {t('wizard.refresh')}
                </button>
              </div>
              {containersLoading ? (
                <div className="py-8 text-center text-xs text-[var(--color-muted-foreground)]">{t('wizard.loading')}</div>
              ) : containers.length === 0 ? (
                <div className="py-8 text-center text-xs text-[var(--color-muted-foreground)]">{t('wizard.noContainers')}</div>
              ) : (
                <div className="space-y-1">
                  {containers.map((container) => (
                    <button key={container.id} type="button" disabled={!container.running} onClick={() => void connectDocker(container.id || container.name)} className="flex w-full items-center gap-2 rounded-[var(--radius-md)] px-2 py-2 text-left text-sm text-[var(--color-foreground)] hover:bg-[var(--color-muted)] disabled:opacity-45">
                      <CubeIcon className="h-4 w-4 shrink-0 text-[var(--color-muted-foreground)]" />
                      <span className="min-w-0 flex-1">
                        <span className="block truncate">{container.name || container.id}</span>
                        <span className="block truncate text-[11px] text-[var(--color-muted-foreground)]">{container.status}</span>
                      </span>
                      {container.running && <span className="text-[10px] font-semibold text-[var(--color-success-fg)]">{t('wizard.running')}</span>}
                    </button>
                  ))}
                </div>
              )}
            </div>
          )}

          {step === 'connecting' && (
            <div className="flex flex-col items-center justify-center py-14 text-sm text-[var(--color-muted-foreground)]">
              <ArrowPathIcon className="mb-3 h-6 w-6 animate-spin" />
              {t('wizard.connecting')}
            </div>
          )}

          {step === 'dir' && (
            <div className="space-y-3">
              <p className="text-xs text-[var(--color-muted-foreground)]">{t('wizard.dirDesc')}</p>
              <div className="rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-background)] px-2 py-1.5 font-mono text-xs text-[var(--color-foreground)]">{currentDir}</div>
              <div className="max-h-64 overflow-y-auto rounded-[var(--radius-lg)] border border-[var(--color-border)] p-1">
                {currentDir !== '/' && <DirRow name=".." onClick={() => navigate('..')} />}
                {dirLoading ? (
                  <div className="py-8 text-center text-xs text-[var(--color-muted-foreground)]">{t('wizard.loading')}</div>
                ) : dirs.length === 0 ? (
                  <div className="py-8 text-center text-xs text-[var(--color-muted-foreground)]">{t('wizard.noSubDirs')}</div>
                ) : (
                  dirs.map((dir) => <DirRow key={dir} name={dir} onClick={() => navigate(dir)} />)
                )}
              </div>
              <Field label={t('wizard.saveAlias')}>
                <input value={aliasName} onChange={(e) => setAliasName(e.target.value)} placeholder={t('wizard.aliasNamePlaceholder')} className={INPUT} />
              </Field>
            </div>
          )}

          {error && <div className="mt-3 rounded-[var(--radius-md)] border border-[var(--color-destructive)] bg-[var(--color-error-bg)] px-3 py-2 text-xs text-[var(--color-error-fg)]">{error}</div>}
        </div>

        <footer className="flex items-center justify-between border-t border-[var(--color-border)] bg-[var(--color-muted)] px-5 py-3">
          <button type="button" onClick={() => {
            if (step === 'method') close()
            else if (step === 'dir' && method === 'ssh') setStep('config')
            else setStep('method')
          }} className="inline-flex items-center gap-1.5 rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-surface)] px-3 py-1.5 text-xs text-[var(--color-foreground)] hover:bg-[var(--color-background)]">
            <ArrowLeftIcon className="h-3.5 w-3.5" />
            {t('common.back')}
          </button>
          {step === 'method' && (
            <button type="button" onClick={() => {
              if (method === 'docker') {
                setStep('docker')
                void loadContainers()
              } else setStep('config')
            }} className="rounded-[var(--radius-md)] bg-[var(--color-primary)] px-3 py-1.5 text-xs font-medium text-[var(--color-on-primary)]">
              {t('wizard.next')}
            </button>
          )}
          {step === 'config' && (
            <button type="button" onClick={() => void connectSSH()} className="rounded-[var(--radius-md)] bg-[var(--color-primary)] px-3 py-1.5 text-xs font-medium text-[var(--color-on-primary)]">
              {t('wizard.connect')}
            </button>
          )}
          {step === 'dir' && (
            <button type="button" disabled={binding} onClick={() => void bindHere()} className="inline-flex items-center gap-1.5 rounded-[var(--radius-md)] bg-[var(--color-primary)] px-3 py-1.5 text-xs font-medium text-[var(--color-on-primary)] disabled:opacity-50">
              <CheckIcon className="h-3.5 w-3.5" />
              {t('wizard.useDirectory')}
            </button>
          )}
        </footer>
      </div>
    </div>
  )
}

const INPUT = 'w-full rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-background)] px-2.5 py-2 text-sm text-[var(--color-foreground)] outline-none focus:border-[var(--color-primary)]'

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <label className="block">
      <span className="mb-1 block text-xs font-medium text-[var(--color-muted-foreground)]">{label}</span>
      {children}
    </label>
  )
}

function MethodCard({ active, Icon, title, desc, onClick }: { active: boolean; Icon: typeof ServerIcon; title: string; desc: string; onClick: () => void }) {
  return (
    <button type="button" onClick={onClick} className={`flex flex-col gap-2 rounded-[var(--radius-xl)] border p-4 text-left transition-colors hover:bg-[var(--color-muted)] ${active ? 'border-[var(--color-primary)] bg-[var(--accent-wash)]' : 'border-[var(--color-border)] bg-[var(--color-background)]'}`}>
      <Icon className="h-5 w-5 text-[var(--color-primary)]" />
      <span className="text-sm font-semibold text-[var(--color-foreground)]">{title}</span>
      <span className="text-xs leading-relaxed text-[var(--color-muted-foreground)]">{desc}</span>
    </button>
  )
}

function DirRow({ name, onClick }: { name: string; onClick: () => void }) {
  return (
    <button type="button" onClick={onClick} className="flex w-full items-center gap-2 rounded-[var(--radius-md)] px-2 py-1.5 text-left text-sm text-[var(--color-foreground)] hover:bg-[var(--color-muted)]">
      {name === '..' ? <span className="w-4 text-center font-mono text-xs">..</span> : <FolderIcon className="h-4 w-4 text-[var(--color-muted-foreground)]" />}
      <span className="min-w-0 flex-1 truncate">{name}</span>
    </button>
  )
}
