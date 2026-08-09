import { useEffect, useId, useMemo, useRef, useState } from 'react'
import {
  ArrowPathIcon,
  ArrowTopRightOnSquareIcon,
  CheckIcon,
  ChevronDownIcon,
  ClipboardDocumentIcon,
  ExclamationTriangleIcon,
  PlusIcon,
  TrashIcon,
  UserCircleIcon,
  XMarkIcon,
} from '@heroicons/react/24/outline'
import { useTranslation } from 'react-i18next'
import { api } from '../../lib/api'
import { openUrl } from '../../lib/useDesktop'
import type {
  AuthMethod,
  ProviderAuthAccount,
  ProviderAuthBinding,
  ProviderAuthFlow,
  ProviderAuthStatus,
  ProviderCredentialMethod,
} from '../../lib/types'
import { ProviderIcon } from '../ProviderIcon'
import {
  BTN_DANGER,
  BTN_GHOST,
  BTN_PRIMARY,
  BTN_SECONDARY,
  BTN_SM,
  BTN_XS,
  CHIP,
  LABEL,
  Segmented,
} from './atoms'

const DEFAULT_ACCOUNT = '__default__'

const METHOD_PROVIDER: Record<AuthMethod, string> = {
  codex_oauth: 'openai',
  xai_oauth: 'xai',
  github_copilot: 'github-copilot',
}

const METHOD_LABEL_KEY: Record<ProviderCredentialMethod, string> = {
  api_key: 'settings.providers.auth.methods.apiKey',
  codex_oauth: 'settings.providers.auth.methods.chatgpt',
  xai_oauth: 'settings.providers.auth.methods.grok',
  github_copilot: 'settings.providers.auth.methods.copilot',
}

const METHOD_SIGN_IN_KEY: Record<AuthMethod, string> = {
  codex_oauth: 'settings.providers.auth.signIn.chatgpt',
  xai_oauth: 'settings.providers.auth.signIn.grok',
  github_copilot: 'settings.providers.auth.signIn.github',
}

type ActiveFlow = ProviderAuthFlow & {
  method: AuthMethod
  bindOnAuthorize: boolean
  generation: number
}

type TerminalState = 'denied' | 'expired' | 'error'

export function providerCredentialMethods(
  declared: ProviderCredentialMethod[] | undefined,
  existing?: ProviderAuthBinding,
): ProviderCredentialMethod[] {
  const methods = declared?.length ? [...declared] : ['api_key' as const]
  if (existing && !methods.includes(existing.method)) methods.push(existing.method)
  return [...new Set(methods)]
}

export function resolveProviderAuthAccount(
  status: ProviderAuthStatus | undefined,
  binding: ProviderAuthBinding | null | undefined,
): ProviderAuthAccount | undefined {
  if (!status || !binding || status.method !== binding.method) return undefined
  const accountID = binding.account_id || status.default_account_id
  if (accountID) return status.accounts.find((account) => account.id === accountID)
  // A single account remains usable with old/partial status responses that did
  // not yet mark it as default. Multiple accounts require an explicit choice.
  return status.accounts.length === 1 ? status.accounts[0] : undefined
}

export function isProviderAuthReady(
  status: ProviderAuthStatus | undefined,
  binding: ProviderAuthBinding | null | undefined,
): boolean {
  const account = resolveProviderAuthAccount(status, binding)
  return !!account && !account.requires_reauth
}

async function copyText(text: string): Promise<void> {
  if (navigator.clipboard?.writeText) {
    await navigator.clipboard.writeText(text)
    return
  }
  const textarea = document.createElement('textarea')
  textarea.value = text
  textarea.setAttribute('readonly', '')
  textarea.style.position = 'fixed'
  textarea.style.opacity = '0'
  document.body.appendChild(textarea)
  textarea.select()
  const copied = document.execCommand?.('copy') ?? false
  textarea.remove()
  if (!copied) throw new Error('copy unavailable')
}

function CopyButton({ value }: { value: string }) {
  const { t } = useTranslation()
  const [copied, setCopied] = useState(false)
  const timeoutRef = useRef<number | null>(null)

  useEffect(() => () => {
    if (timeoutRef.current !== null) window.clearTimeout(timeoutRef.current)
  }, [])

  async function copy() {
    try {
      await copyText(value)
      setCopied(true)
      if (timeoutRef.current !== null) window.clearTimeout(timeoutRef.current)
      timeoutRef.current = window.setTimeout(() => setCopied(false), 1500)
    } catch {
      setCopied(false)
    }
  }

  return (
    <button
      type="button"
      onClick={() => void copy()}
      className={`${BTN_SECONDARY} ${BTN_XS}`}
      aria-label={copied ? t('settings.providers.auth.copied') : t('settings.providers.auth.copyCode')}
    >
      {copied ? <CheckIcon className="h-3.5 w-3.5" /> : <ClipboardDocumentIcon className="h-3.5 w-3.5" />}
      {copied ? t('settings.providers.auth.copied') : t('settings.providers.auth.copyCode')}
    </button>
  )
}

export function DeviceCodePanel({
  flow,
  browserOpenFailed,
  completing,
  onOpen,
  onCancel,
}: {
  flow: ProviderAuthFlow
  browserOpenFailed: boolean
  completing: boolean
  onOpen: () => void
  onCancel: () => void
}) {
  const { t } = useTranslation()
  const panelRef = useRef<HTMLElement | null>(null)
  const titleID = useId()
  const hintID = useId()
  const codeID = useId()
  const verificationURL = flow.verification_uri_complete || flow.verification_uri
  const expiresAt = new Date(flow.expires_at)
  const expiryLabel = Number.isNaN(expiresAt.getTime())
    ? ''
    : t('settings.providers.auth.expiresAt', {
        time: expiresAt.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }),
      })

  useEffect(() => {
    panelRef.current?.focus()
  }, [])

  return (
    <section
      ref={panelRef}
      tabIndex={-1}
      aria-labelledby={titleID}
      aria-describedby={`${hintID} ${codeID}`}
      aria-busy="true"
      className="rounded-[var(--radius-lg)] border border-[var(--color-border)] bg-[var(--color-background)] p-3.5 outline-none focus-visible:ring-2 focus-visible:ring-[var(--color-primary)]"
    >
      <div className="flex items-start gap-3">
        <div className="grid h-8 w-8 shrink-0 place-items-center rounded-[var(--radius-md)] bg-[var(--color-secondary)] text-[var(--color-foreground)]">
          <ArrowTopRightOnSquareIcon className="h-4 w-4" />
        </div>
        <div className="min-w-0 flex-1">
          <h3 id={titleID} className="text-[12px] font-semibold text-[var(--color-foreground)]">
            {t('settings.providers.auth.enterCodeTitle')}
          </h3>
          <div id={hintID} className="mt-0.5 text-[10.5px] leading-relaxed text-[var(--color-muted-foreground)]">
            {t('settings.providers.auth.enterCodeHint')}
          </div>
        </div>
      </div>

      <div className="mt-3 flex flex-wrap items-center gap-2 rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-surface)] px-3 py-2.5">
        <code id={codeID} className="mr-auto font-mono text-base font-semibold tracking-[0.18em] text-[var(--color-foreground)]">
          {flow.user_code}
        </code>
        <CopyButton value={flow.user_code} />
      </div>

      <button
        type="button"
        onClick={onOpen}
        className="mt-2 flex max-w-full items-center gap-1.5 text-left text-[11px] font-medium text-[var(--color-accent-neutral)] hover:underline"
        title={verificationURL}
      >
        <ArrowTopRightOnSquareIcon className="h-3.5 w-3.5 shrink-0" />
        <span className="truncate">{flow.verification_uri}</span>
      </button>

      {browserOpenFailed && (
        <div role="alert" className="mt-2 text-[10.5px] text-[var(--color-warning-fg)]">
          {t('settings.providers.auth.browserOpenFailed')}
        </div>
      )}

      <div className="mt-3 flex items-center justify-between gap-3 border-t border-[var(--color-border)] pt-2.5">
        <div role="status" aria-live="polite" className="flex min-w-0 items-center gap-1.5 text-[10.5px] text-[var(--color-muted-foreground)]">
          <ArrowPathIcon className="h-3.5 w-3.5 shrink-0 animate-spin motion-reduce:animate-none" />
          <span>{completing ? t('settings.providers.auth.finishing') : t('settings.providers.auth.waiting')}</span>
          {expiryLabel && <span className="hidden sm:inline">· {expiryLabel}</span>}
        </div>
        <button type="button" onClick={onCancel} disabled={completing} className={`${BTN_GHOST} ${BTN_XS}`}>
          <XMarkIcon className="h-3.5 w-3.5" />
          {t('common.cancel')}
        </button>
      </div>
    </section>
  )
}

export interface ProviderAuthSectionProps {
  methods: ProviderCredentialMethod[]
  value: ProviderCredentialMethod
  binding: ProviderAuthBinding | null
  initialStatus?: ProviderAuthStatus
  apiKeyField: React.ReactNode
  disabled?: boolean
  onMethodChange: (method: ProviderCredentialMethod) => void
  onBindingChange: (binding: ProviderAuthBinding | null) => void
  onStatusChange?: (status: ProviderAuthStatus) => void
  onAuthenticated?: (status: ProviderAuthStatus) => void | Promise<void>
}

export function ProviderAuthSection({
  methods,
  value,
  binding,
  initialStatus,
  apiKeyField,
  disabled = false,
  onMethodChange,
  onBindingChange,
  onStatusChange,
  onAuthenticated,
}: ProviderAuthSectionProps) {
  const { t } = useTranslation()
  const oauthMethod = value === 'api_key' ? null : value
  const methodRef = useRef<AuthMethod | null>(oauthMethod)
  const bindingRef = useRef(binding)
  const initialStatusRef = useRef(initialStatus)
  const onBindingChangeRef = useRef(onBindingChange)
  const onStatusChangeRef = useRef(onStatusChange)
  const onAuthenticatedRef = useRef(onAuthenticated)
  const mountedRef = useRef(true)
  const disabledRef = useRef(disabled)
  const generationRef = useRef(0)
  const statusRequestEpochRef = useRef(0)
  const requestSequenceRef = useRef(0)
  const startInFlightRef = useRef<{ requestID: number; generation: number } | null>(null)
  const actionInFlightRef = useRef<{ requestID: number; generation: number } | null>(null)
  const activeFlowRef = useRef<{ method: AuthMethod; flowID: string; generation: number } | null>(null)
  const [status, setStatus] = useState<ProviderAuthStatus | undefined>(
    initialStatus?.method === oauthMethod ? initialStatus : undefined,
  )
  const statusRef = useRef(status)
  const [statusLoading, setStatusLoading] = useState(false)
  const [flow, setFlow] = useState<ActiveFlow | null>(null)
  const [starting, setStarting] = useState(false)
  const [completingFlow, setCompletingFlow] = useState(false)
  const [busyAction, setBusyAction] = useState('')
  const [actionError, setActionError] = useState('')
  const [postAuthError, setPostAuthError] = useState('')
  const [terminal, setTerminal] = useState<{ state: TerminalState; message?: string } | null>(null)
  const [browserOpenFailed, setBrowserOpenFailed] = useState(false)
  const [manageOpen, setManageOpen] = useState(false)
  const [confirmRemove, setConfirmRemove] = useState('')
  const [confirmLogout, setConfirmLogout] = useState(false)
  const accountSelectID = useId()
  const accountsPanelID = useId()
  const removeConfirmLabelID = useId()
  const logoutConfirmLabelID = useId()
  const removeConfirmButtonRef = useRef<HTMLButtonElement | null>(null)
  const logoutConfirmButtonRef = useRef<HTMLButtonElement | null>(null)

  methodRef.current = oauthMethod
  bindingRef.current = binding
  disabledRef.current = disabled
  initialStatusRef.current = initialStatus
  onBindingChangeRef.current = onBindingChange
  onStatusChangeRef.current = onStatusChange
  onAuthenticatedRef.current = onAuthenticated
  statusRef.current = status

  const normalizedMethods = useMemo(
    () => [...new Set(methods.length ? methods : ['api_key' as const])],
    [methods],
  )

  function isCurrent(method: AuthMethod, generation: number): boolean {
    return mountedRef.current && methodRef.current === method && generationRef.current === generation
  }

  function nextStatusRequest(): number {
    statusRequestEpochRef.current += 1
    return statusRequestEpochRef.current
  }

  function isStatusRequestCurrent(requestEpoch: number): boolean {
    return statusRequestEpochRef.current === requestEpoch
  }

  function cancelActiveFlow() {
    const active = activeFlowRef.current
    activeFlowRef.current = null
    if (active) void api.cancelProviderAuthFlow(active.method, active.flowID).catch(() => {})
    return active
  }

  useEffect(() => {
    mountedRef.current = true
    return () => {
      mountedRef.current = false
      generationRef.current += 1
      cancelActiveFlow()
    }
  }, [])

  useEffect(() => {
    const generation = generationRef.current + 1
    generationRef.current = generation
    const statusRequest = nextStatusRequest()
    cancelActiveFlow()
    setFlow(null)
    setStarting(false)
    setCompletingFlow(false)
    setBusyAction('')
    setPostAuthError('')
    if (!oauthMethod) {
      statusRef.current = undefined
      setStatus(undefined)
      setActionError('')
      setTerminal(null)
      return
    }
    let alive = true
    const initial = initialStatusRef.current
    const seeded = initial?.method === oauthMethod ? initial : undefined
    statusRef.current = seeded
    setStatus(seeded)
    setStatusLoading(!seeded)
    setActionError('')
    setTerminal(null)
    api.providerAuthStatus(oauthMethod)
      .then((next) => {
        if (!alive || !isCurrent(oauthMethod, generation) || !isStatusRequestCurrent(statusRequest)) return
        statusRef.current = next
        setStatus(next)
        onStatusChangeRef.current?.(next)
      })
      .catch((err) => {
        if (!alive || !isCurrent(oauthMethod, generation) || !isStatusRequestCurrent(statusRequest)) return
        setActionError(err instanceof Error ? err.message : String(err))
      })
      .finally(() => {
        if (alive && isCurrent(oauthMethod, generation) && isStatusRequestCurrent(statusRequest)) {
          setStatusLoading(false)
        }
      })
    return () => {
      alive = false
    }
  }, [oauthMethod])

  useEffect(() => {
    if (confirmRemove) removeConfirmButtonRef.current?.focus()
  }, [confirmRemove])

  useEffect(() => {
    if (confirmLogout) logoutConfirmButtonRef.current?.focus()
  }, [confirmLogout])

  useEffect(() => {
    if (!flow) return
    let stopped = false
    let timer: number | null = null
    const initialIntervalMS = Math.max(1, flow.interval_seconds ?? flow.interval ?? 5) * 1000
    async function poll() {
      if (stopped) return
      const expiry = Date.parse(flow!.expires_at)
      if (Number.isFinite(expiry) && Date.now() >= expiry) {
        if (!isCurrent(flow!.method, flow!.generation)) return
        activeFlowRef.current = null
        setFlow(null)
        setCompletingFlow(false)
        setTerminal({ state: 'expired' })
        return
      }
      try {
        const result = await api.pollProviderAuthFlow(flow!.method, flow!.flow_id)
        if (stopped || !isCurrent(flow!.method, flow!.generation)) return
        if (result.state === 'pending') {
          const nextIntervalMS = Math.max(
            1,
            result.interval_seconds ?? result.interval ?? flow!.interval_seconds ?? flow!.interval ?? 5,
          ) * 1000
          timer = window.setTimeout(() => void poll(), nextIntervalMS)
          return
        }
        if (result.state !== 'authorized') {
          activeFlowRef.current = null
          setFlow(null)
          setCompletingFlow(false)
          setTerminal({ state: result.state, message: result.error })
          return
        }

        setCompletingFlow(true)
        const statusRequest = nextStatusRequest()
        let next: ProviderAuthStatus | undefined
        let refreshFailure = ''
        try {
          next = await api.providerAuthStatus(flow!.method)
        } catch (err) {
          refreshFailure = err instanceof Error ? err.message : String(err)
          if (result.account) {
            const previous = statusRef.current?.method === flow!.method ? statusRef.current : undefined
            const accounts = previous?.accounts.filter((account) => account.id !== result.account!.id) ?? []
            accounts.push(result.account)
            next = {
              method: flow!.method,
              accounts,
              default_account_id: previous?.default_account_id || result.account.id,
            }
          }
        }
        if (stopped || !isCurrent(flow!.method, flow!.generation)
          || !isStatusRequestCurrent(statusRequest)) return
        if (!next) {
          setPostAuthError(t('settings.providers.auth.postAuthRefreshFailed', { reason: refreshFailure }))
          activeFlowRef.current = null
          setFlow(null)
          setCompletingFlow(false)
          return
        }
        statusRef.current = next
        setStatus(next)
        setTerminal(null)
        onStatusChangeRef.current?.(next)
        if (flow!.bindOnAuthorize && result.account) {
          onBindingChangeRef.current({ method: flow!.method, account_id: result.account.id })
        }
        try {
          await onAuthenticatedRef.current?.(next)
        } catch (err) {
          refreshFailure = err instanceof Error ? err.message : String(err)
        }
        if (stopped || !isCurrent(flow!.method, flow!.generation)) return
        setPostAuthError(refreshFailure
          ? t('settings.providers.auth.postAuthRefreshFailed', { reason: refreshFailure })
          : '')
        activeFlowRef.current = null
        setFlow(null)
        setCompletingFlow(false)
      } catch (err) {
        if (stopped || !isCurrent(flow!.method, flow!.generation)) return
        activeFlowRef.current = null
        setFlow(null)
        setCompletingFlow(false)
        setTerminal({ state: 'error', message: err instanceof Error ? err.message : String(err) })
      }
    }

    timer = window.setTimeout(() => void poll(), initialIntervalMS)
    return () => {
      stopped = true
      if (timer !== null) window.clearTimeout(timer)
    }
  }, [flow])

  function selectMethod(method: ProviderCredentialMethod) {
    if (method === value || disabledRef.current) return
    generationRef.current += 1
    nextStatusRequest()
    cancelActiveFlow()
    setFlow(null)
    setStarting(false)
    setCompletingFlow(false)
    setBusyAction('')
    setPostAuthError('')
    onMethodChange(method)
    onBindingChange(method === 'api_key' ? null : { method })
  }

  async function openVerification(uri: string, method: AuthMethod, generation: number) {
    if (!isCurrent(method, generation)) return
    try {
      await openUrl(uri)
      if (!isCurrent(method, generation)) return
      setBrowserOpenFailed(false)
    } catch {
      if (!isCurrent(method, generation)) return
      setBrowserOpenFailed(true)
    }
  }

  async function startLogin() {
    const method = methodRef.current
    if (!method || disabledRef.current) return
    const generation = generationRef.current
    if (startInFlightRef.current?.generation === generation) return
    nextStatusRequest()
    const requestID = requestSequenceRef.current + 1
    requestSequenceRef.current = requestID
    startInFlightRef.current = { requestID, generation }
    setStarting(true)
    setActionError('')
    setPostAuthError('')
    setTerminal(null)
    setBrowserOpenFailed(false)
    try {
      const started = await api.startProviderAuth(method)
      if (!isCurrent(method, generation)
        || startInFlightRef.current?.requestID !== requestID) {
        void api.cancelProviderAuthFlow(method, started.flow_id).catch(() => {})
        return
      }
      const active: ActiveFlow = {
        ...started,
        method,
        bindOnAuthorize: !isProviderAuthReady(statusRef.current, bindingRef.current),
        generation,
      }
      activeFlowRef.current = { method, flowID: started.flow_id, generation }
      setFlow(active)
      void openVerification(started.verification_uri_complete || started.verification_uri, method, generation)
    } catch (err) {
      if (!isCurrent(method, generation)
        || startInFlightRef.current?.requestID !== requestID) return
      setActionError(err instanceof Error ? err.message : String(err))
    } finally {
      if (startInFlightRef.current?.requestID === requestID) {
        startInFlightRef.current = null
        if (isCurrent(method, generation)) setStarting(false)
      }
    }
  }

  async function cancelLogin() {
    const active = activeFlowRef.current
    activeFlowRef.current = null
    setFlow(null)
    setCompletingFlow(false)
    if (!active) return
    try {
      await api.cancelProviderAuthFlow(active.method, active.flowID)
    } catch {
      // Cancellation is best-effort; the local flow is already closed.
    }
  }

  async function runStatusAction(
    action: string,
    method: AuthMethod,
    request: () => Promise<ProviderAuthStatus>,
    onCommit?: () => void,
  ) {
    if (disabledRef.current) return
    const generation = generationRef.current
    nextStatusRequest()
    const requestID = requestSequenceRef.current + 1
    requestSequenceRef.current = requestID
    actionInFlightRef.current = { requestID, generation }
    setBusyAction(action)
    setActionError('')
    setPostAuthError('')
    try {
      const next = await request()
      if (!isCurrent(method, generation)
        || actionInFlightRef.current?.requestID !== requestID
        || next.method !== method) return
      statusRef.current = next
      setStatus(next)
      onStatusChangeRef.current?.(next)
      onCommit?.()
      try {
        await onAuthenticatedRef.current?.(next)
      } catch (err) {
        if (isCurrent(method, generation)) {
          const reason = err instanceof Error ? err.message : String(err)
          setPostAuthError(t('settings.providers.auth.postAuthRefreshFailed', { reason }))
        }
      }
    } catch (err) {
      if (!isCurrent(method, generation)
        || actionInFlightRef.current?.requestID !== requestID) return
      setActionError(err instanceof Error ? err.message : String(err))
    } finally {
      if (actionInFlightRef.current?.requestID === requestID) {
        actionInFlightRef.current = null
        if (isCurrent(method, generation)) setBusyAction('')
      }
    }
  }

  async function makeDefault(accountID: string) {
    if (!oauthMethod) return
    await runStatusAction(
      `default:${accountID}`,
      oauthMethod,
      () => api.setProviderAuthDefault(oauthMethod, accountID),
    )
  }

  async function removeAccount(accountID: string) {
    if (!oauthMethod) return
    await runStatusAction(
      `remove:${accountID}`,
      oauthMethod,
      () => api.removeProviderAuthAccount(oauthMethod, accountID),
      () => setConfirmRemove(''),
    )
  }

  async function logoutAll() {
    if (!oauthMethod) return
    await runStatusAction(
      'logout',
      oauthMethod,
      () => api.logoutProviderAuth(oauthMethod),
      () => {
        setConfirmLogout(false)
        setManageOpen(false)
      },
    )
  }

  const boundAccount = resolveProviderAuthAccount(status, binding)
  const bindingMissing = !!status && !!binding?.account_id && !boundAccount
  const effectiveDefault = status?.accounts.find((account) => account.id === status.default_account_id)
    ?? (status?.accounts.length === 1 ? status.accounts[0] : undefined)
  const isConnected = !!boundAccount && !boundAccount.requires_reauth
  const methodLabel = oauthMethod ? t(METHOD_LABEL_KEY[oauthMethod]) : ''
  const interactionDisabled = disabled || starting || !!busyAction || completingFlow

  return (
    <fieldset disabled={interactionDisabled} className="mb-3.5 min-w-0 disabled:opacity-70">
      <legend className={LABEL}>{t('settings.providers.auth.title')}</legend>
      {normalizedMethods.length > 1 ? (
        <div className="max-w-full overflow-x-auto">
          <Segmented
            value={value}
            options={normalizedMethods.map((method) => ({ value: method, label: t(METHOD_LABEL_KEY[method]) }))}
            onChange={selectMethod}
          />
        </div>
      ) : (
        <div aria-label={t('settings.providers.auth.title')} className="inline-flex h-6 items-center rounded-[var(--radius-md)] bg-[var(--color-muted)] px-2.5 text-[11px] font-medium text-[var(--color-foreground)]">
          {t(METHOD_LABEL_KEY[normalizedMethods[0]])}
        </div>
      )}

      {value === 'api_key' ? (
        <div className="mt-2">
          <label className={LABEL}>{t('settings.providers.apiKey')}</label>
          {apiKeyField}
        </div>
      ) : (
        <div className="mt-2 rounded-[var(--radius-lg)] border border-[var(--color-border)] bg-[var(--color-muted)] p-3">
          <div aria-live="polite" className="flex items-start gap-3">
            <div className="grid h-8 w-8 shrink-0 place-items-center rounded-[var(--radius-md)] bg-[var(--color-surface)] text-[var(--color-foreground)]">
              <ProviderIcon provider={METHOD_PROVIDER[value]} size={18} />
            </div>
            <div className="min-w-0 flex-1">
              <div className="flex flex-wrap items-center gap-1.5">
                <span className="text-[12px] font-semibold text-[var(--color-foreground)]">{methodLabel}</span>
                {isConnected && (
                  <span className={`${CHIP} !bg-[var(--color-success-bg)] !text-[var(--color-success-fg)]`}>
                    {t('settings.providers.auth.connected')}
                  </span>
                )}
                {(boundAccount?.requires_reauth || bindingMissing) && (
                  <span className={`${CHIP} !bg-[var(--color-warning-bg)] !text-[var(--color-warning-fg)]`}>
                    {t('settings.providers.auth.needsReauth')}
                  </span>
                )}
              </div>
              <div className="mt-0.5 text-[10.5px] leading-relaxed text-[var(--color-muted-foreground)]">
                {t(`settings.providers.auth.descriptions.${value}`)}
              </div>
            </div>
          </div>

          {statusLoading ? (
            <div className="mt-3 flex items-center gap-2 text-[11px] text-[var(--color-muted-foreground)]" role="status" aria-live="polite">
              <ArrowPathIcon className="h-3.5 w-3.5 animate-spin motion-reduce:animate-none" />
              {t('settings.providers.auth.loadingAccounts')}
            </div>
          ) : flow ? (
            <div className="mt-3">
              <DeviceCodePanel
                flow={flow}
                browserOpenFailed={browserOpenFailed}
                completing={completingFlow}
                onOpen={() => void openVerification(
                  flow.verification_uri_complete || flow.verification_uri,
                  flow.method,
                  flow.generation,
                )}
                onCancel={() => void cancelLogin()}
              />
            </div>
          ) : terminal ? null : status?.accounts.length ? (
            <div className="mt-3">
              <label htmlFor={accountSelectID} className={LABEL}>{t('settings.providers.auth.account')}</label>
              <div className="flex flex-col gap-2 sm:flex-row sm:items-center">
                <select
                  id={accountSelectID}
                  value={binding?.method === value && binding.account_id ? binding.account_id : DEFAULT_ACCOUNT}
                  onChange={(event) => onBindingChange({
                    method: value,
                    account_id: event.target.value === DEFAULT_ACCOUNT ? undefined : event.target.value,
                  })}
                  disabled={disabled}
                  aria-label={t('settings.providers.auth.account')}
                  className="h-8 min-w-0 w-full flex-1 rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-surface)] px-2.5 text-xs text-[var(--color-foreground)] outline-none focus:border-[var(--color-primary)] sm:w-auto"
                >
                  <option value={DEFAULT_ACCOUNT} disabled={!effectiveDefault}>
                    {effectiveDefault
                      ? t('settings.providers.auth.useDefaultAccount', { account: effectiveDefault.login })
                      : t('settings.providers.auth.noDefaultAccount')}
                  </option>
                  {bindingMissing && binding?.account_id && (
                    <option value={binding.account_id} disabled>
                      {t('settings.providers.auth.missingAccountOption')}
                    </option>
                  )}
                  {status.accounts.map((account) => (
                    <option key={account.id} value={account.id} disabled={account.requires_reauth}>
                      {account.login}{account.domain ? ` · ${account.domain}` : ''}{account.requires_reauth ? ` · ${t('settings.providers.auth.needsReauth')}` : ''}
                    </option>
                  ))}
                </select>
                <button type="button" onClick={() => void startLogin()} disabled={starting || disabled} className={`${BTN_SECONDARY} ${BTN_SM}`}>
                  <PlusIcon className="h-3.5 w-3.5" />
                  {t('settings.providers.auth.addAccount')}
                </button>
              </div>

              {boundAccount?.requires_reauth && (
                <div className="mt-2 flex items-center justify-between gap-3 rounded-[var(--radius-md)] bg-[var(--color-warning-bg)] px-2.5 py-2 text-[10.5px] text-[var(--color-warning-fg)]">
                  <span>{t('settings.providers.auth.reauthHint', { account: boundAccount.login })}</span>
                  <button type="button" onClick={() => void startLogin()} disabled={starting || disabled} className={`${BTN_SECONDARY} ${BTN_XS}`}>
                    <ArrowPathIcon className="h-3.5 w-3.5" />
                    {t('settings.providers.auth.reauthenticate')}
                  </button>
                </div>
              )}

              {!boundAccount && status.accounts.length > 0 && (
                <div className="mt-2 flex items-center justify-between gap-3 rounded-[var(--radius-md)] bg-[var(--color-warning-bg)] px-2.5 py-2 text-[10.5px] text-[var(--color-warning-fg)]">
                  <span>
                    {bindingMissing
                      ? t('settings.providers.auth.missingAccountHint')
                      : t('settings.providers.auth.chooseAccountHint')}
                  </span>
                  {bindingMissing && (
                    <button type="button" onClick={() => void startLogin()} disabled={starting || disabled} className={`${BTN_SECONDARY} ${BTN_XS}`}>
                      <ArrowPathIcon className="h-3.5 w-3.5" />
                      {t('settings.providers.auth.reauthenticate')}
                    </button>
                  )}
                </div>
              )}

              <button
                type="button"
                onClick={() => setManageOpen((open) => !open)}
                className="mt-2 flex items-center gap-1 text-[10.5px] font-medium text-[var(--color-muted-foreground)] hover:text-[var(--color-foreground)]"
                aria-expanded={manageOpen}
                aria-controls={accountsPanelID}
              >
                <ChevronDownIcon className={`h-3.5 w-3.5 transition-transform motion-reduce:transition-none ${manageOpen ? 'rotate-180' : ''}`} />
                {t('settings.providers.auth.manageAccounts', { count: status.accounts.length })}
              </button>

              {manageOpen && (
                <div id={accountsPanelID} className="mt-2 space-y-1.5 border-t border-[var(--color-border)] pt-2">
                  {status.accounts.map((account) => {
                    const isDefault = account.id === status.default_account_id
                    const isBound = binding?.method === value
                      && (binding.account_id === account.id || (!binding.account_id && effectiveDefault?.id === account.id))
                    return (
                      <div key={account.id} className="rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-surface)] px-2.5 py-2">
                        <div className="flex flex-wrap items-center gap-2">
                          <UserCircleIcon className="h-4 w-4 shrink-0 text-[var(--color-muted-foreground)]" />
                          <div className="min-w-[8rem] flex-1">
                            <div className="truncate text-[11px] font-medium text-[var(--color-foreground)]">{account.login}</div>
                            <div className="truncate text-[10px] text-[var(--color-muted-foreground)]">
                              {[account.email, account.domain].filter(Boolean).join(' · ') || t('settings.providers.auth.managedAccount')}
                            </div>
                          </div>
                          {isDefault && <span className={CHIP}>{t('settings.providers.auth.defaultBadge')}</span>}
                          {isBound && <span className={CHIP}>{t('settings.providers.auth.boundBadge')}</span>}
                          {account.requires_reauth && (
                            <span className={`${CHIP} !bg-[var(--color-warning-bg)] !text-[var(--color-warning-fg)]`}>
                              {t('settings.providers.auth.needsReauth')}
                            </span>
                          )}
                        </div>
                        <div className="mt-2 flex flex-wrap justify-end gap-1.5">
                          {!isDefault && !account.requires_reauth && (
                            <button
                              type="button"
                              disabled={!!busyAction}
                              onClick={() => void makeDefault(account.id)}
                              className={`${BTN_GHOST} ${BTN_XS}`}
                            >
                              {busyAction === `default:${account.id}` && <ArrowPathIcon className="h-3 w-3 animate-spin motion-reduce:animate-none" />}
                              {t('settings.providers.auth.setDefault')}
                            </button>
                          )}
                          {confirmRemove === account.id ? (
                            <div role="group" aria-labelledby={removeConfirmLabelID} className="flex flex-wrap items-center justify-end gap-1.5">
                              <span id={removeConfirmLabelID} className="self-center text-[10px] text-[var(--color-warning-fg)]">
                                {t('settings.providers.auth.removeAccountConfirm')}
                              </span>
                              <button type="button" className={`${BTN_GHOST} ${BTN_XS}`} onClick={() => setConfirmRemove('')}>
                                {t('common.cancel')}
                              </button>
                              <button
                                ref={removeConfirmButtonRef}
                                type="button"
                                disabled={!!busyAction}
                                className={`${BTN_DANGER} ${BTN_XS}`}
                                onClick={() => void removeAccount(account.id)}
                              >
                                {busyAction === `remove:${account.id}` && <ArrowPathIcon className="h-3 w-3 animate-spin motion-reduce:animate-none" />}
                                {t('common.remove')}
                              </button>
                            </div>
                          ) : (
                            <button
                              type="button"
                              disabled={!!busyAction}
                              onClick={() => setConfirmRemove(account.id)}
                              className={`${BTN_GHOST} ${BTN_XS} text-[var(--color-muted-foreground)] hover:text-[var(--color-destructive)]`}
                            >
                              <TrashIcon className="h-3.5 w-3.5" />
                              {t('settings.providers.auth.removeAccount')}
                            </button>
                          )}
                        </div>
                      </div>
                    )
                  })}

                  <div className="flex justify-end pt-1">
                    {confirmLogout ? (
                      <div role="group" aria-labelledby={logoutConfirmLabelID} className="flex flex-wrap items-center justify-end gap-1.5">
                        <span id={logoutConfirmLabelID} className="text-[10px] text-[var(--color-warning-fg)]">{t('settings.providers.auth.logoutAllConfirm')}</span>
                        <button type="button" className={`${BTN_GHOST} ${BTN_XS}`} onClick={() => setConfirmLogout(false)}>
                          {t('common.cancel')}
                        </button>
                        <button ref={logoutConfirmButtonRef} type="button" disabled={!!busyAction} className={`${BTN_DANGER} ${BTN_XS}`} onClick={() => void logoutAll()}>
                          {busyAction === 'logout' && <ArrowPathIcon className="h-3 w-3 animate-spin motion-reduce:animate-none" />}
                          {t('settings.providers.auth.logoutAll')}
                        </button>
                      </div>
                    ) : (
                      <button type="button" disabled={!!busyAction} className={`${BTN_GHOST} ${BTN_XS}`} onClick={() => setConfirmLogout(true)}>
                        {t('settings.providers.auth.logoutAll')}
                      </button>
                    )}
                  </div>
                </div>
              )}
            </div>
          ) : (
            <div className="mt-3 flex flex-col gap-2 rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-surface)] px-3 py-2.5 sm:flex-row sm:items-center">
              <div className="min-w-0 flex-1">
                <div className="text-[11px] font-medium text-[var(--color-foreground)]">{t('settings.providers.auth.notSignedIn')}</div>
                <div className="mt-0.5 text-[10px] text-[var(--color-muted-foreground)]">{t('settings.providers.auth.signInHint')}</div>
              </div>
              <button type="button" onClick={() => void startLogin()} disabled={starting || disabled} className={`${BTN_PRIMARY} ${BTN_SM}`}>
                {starting ? <ArrowPathIcon className="h-3.5 w-3.5 animate-spin motion-reduce:animate-none" /> : <ArrowTopRightOnSquareIcon className="h-3.5 w-3.5" />}
                {t(METHOD_SIGN_IN_KEY[value])}
              </button>
            </div>
          )}

          {terminal && !flow && (
            <div role="alert" className="mt-2 flex items-start gap-2 rounded-[var(--radius-md)] bg-[var(--color-warning-bg)] px-2.5 py-2 text-[10.5px] text-[var(--color-warning-fg)]">
              <ExclamationTriangleIcon className="mt-0.5 h-3.5 w-3.5 shrink-0" />
              <div className="min-w-0 flex-1">
                {terminal.message || t(`settings.providers.auth.flow.${terminal.state}`)}
              </div>
              <button type="button" onClick={() => void startLogin()} disabled={starting || disabled} className={`${BTN_SECONDARY} ${BTN_XS}`}>
                {t('settings.providers.auth.retry')}
              </button>
            </div>
          )}

          {actionError && (
            <div role="alert" className="mt-2 flex items-start gap-2 text-[10.5px] text-[var(--color-destructive)]">
              <ExclamationTriangleIcon className="mt-0.5 h-3.5 w-3.5 shrink-0" />
              <span className="min-w-0 break-words">{actionError}</span>
            </div>
          )}

          {postAuthError && (
            <div role="alert" className="mt-2 flex items-start gap-2 rounded-[var(--radius-md)] bg-[var(--color-warning-bg)] px-2.5 py-2 text-[10.5px] text-[var(--color-warning-fg)]">
              <ExclamationTriangleIcon className="mt-0.5 h-3.5 w-3.5 shrink-0" />
              <span className="min-w-0 break-words">{postAuthError}</span>
            </div>
          )}
        </div>
      )}
    </fieldset>
  )
}
