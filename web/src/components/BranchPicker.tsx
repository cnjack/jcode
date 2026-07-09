import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import {
  CheckIcon,
  ChevronDownIcon,
  CodeBracketIcon,
  MagnifyingGlassIcon,
  PlusIcon,
} from '@heroicons/react/24/outline'
import { useTranslation } from 'react-i18next'
import { api } from '../lib/api'
import { useAppSelector } from '../app/hooks'

interface PendingSwitch {
  branch: string
  create: boolean
  files: string[]
}

const MAX_FILES = 8

export function BranchPicker({ placement = 'top' }: { placement?: 'top' | 'bottom' }) {
  const { t } = useTranslation()
  const projectPath = useAppSelector((s) => s.session.projectPath)
  const [current, setCurrent] = useState('')
  const [branches, setBranches] = useState<string[]>([])
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState('')
  const [creating, setCreating] = useState(false)
  const [newName, setNewName] = useState('')
  const [switching, setSwitching] = useState(false)
  const [error, setError] = useState('')
  const [pending, setPending] = useState<PendingSwitch | null>(null)
  const rootRef = useRef<HTMLDivElement | null>(null)
  const inputRef = useRef<HTMLInputElement | null>(null)

  const refresh = useCallback(async () => {
    try {
      const res = await api.gitBranches()
      setCurrent(res.current || '')
      setBranches(res.branches || [])
    } catch {
      setCurrent('')
      setBranches([])
    }
  }, [])

  useEffect(() => {
    void refresh()
  }, [refresh, projectPath])

  useEffect(() => {
    function onFocus() {
      void refresh()
    }
    function onVisible() {
      if (document.visibilityState === 'visible') void refresh()
    }
    window.addEventListener('focus', onFocus)
    document.addEventListener('visibilitychange', onVisible)
    return () => {
      window.removeEventListener('focus', onFocus)
      document.removeEventListener('visibilitychange', onVisible)
    }
  }, [refresh])

  useEffect(() => {
    if (!open) return
    function onDown(e: MouseEvent) {
      if (rootRef.current && !rootRef.current.contains(e.target as Node)) reset()
    }
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') reset()
    }
    document.addEventListener('mousedown', onDown)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('mousedown', onDown)
      document.removeEventListener('keydown', onKey)
    }
  }, [open])

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase()
    if (!q) return branches
    return branches.filter((b) => b.toLowerCase().includes(q))
  }, [branches, query])

  if (!current) return null

  function reset() {
    setOpen(false)
    setQuery('')
    setCreating(false)
    setNewName('')
    setError('')
    setPending(null)
  }

  async function checkout(branch: string, create = false, strategy: '' | 'stash' | 'force' = '') {
    if (!branch.trim()) return
    if (branch === current && !create) {
      reset()
      return
    }
    setSwitching(true)
    setError('')
    try {
      const res = await api.gitCheckout(branch.trim(), create, strategy)
      if (res.blocked) {
        setPending({ branch: branch.trim(), create, files: res.files || [] })
        return
      }
      setPending(null)
      setCurrent(res.branch || branch.trim())
      await refresh()
      reset()
    } catch (e) {
      setError(e instanceof Error ? e.message : t('errors.branchSwitch'))
    } finally {
      setSwitching(false)
    }
  }

  function startCreate() {
    setCreating(true)
    setNewName(query.trim())
    setTimeout(() => inputRef.current?.focus(), 0)
  }

  return (
    <div ref={rootRef} className="relative inline-flex min-w-0">
      <button
        type="button"
        disabled={switching}
        title={t('branches.current').replace('{name}', current)}
        onClick={(e) => {
          e.stopPropagation()
          setOpen((v) => !v)
        }}
        className="inline-flex h-7 max-w-[180px] items-center gap-1.5 rounded-[var(--radius-lg)] px-2 text-xs text-[var(--color-muted-foreground)] transition-colors hover:bg-[var(--color-muted)] hover:text-[var(--color-foreground)] disabled:opacity-55"
      >
        <CodeBracketIcon className="h-3.5 w-3.5 shrink-0" />
        <span className="truncate font-mono">{current}</span>
        <ChevronDownIcon className={`h-3 w-3 shrink-0 opacity-70 transition-transform ${open ? 'rotate-180' : ''}`} />
      </button>

      {open && (
        <div className={`absolute left-0 z-[var(--z-dropdown)] w-[300px] max-w-[84vw] rounded-[var(--radius-xl)] border border-[var(--color-border)] bg-[var(--color-surface)] p-1.5 shadow-[var(--shadow-lg)] ${
          placement === 'bottom' ? 'top-full mt-1' : 'bottom-full mb-1'
        }`}>
          {pending ? (
            <div className="flex flex-col gap-2 p-1">
              <div className="text-xs font-semibold text-[var(--color-foreground)]">{t('branches.confirmTitle')}</div>
              <div className="text-[11.5px] text-[var(--color-muted-foreground)]">
                {t('branches.confirmIntro').replace('{branch}', pending.branch)}
              </div>
              {pending.files.length > 0 && (
                <ul className="max-h-32 overflow-y-auto rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-muted)] p-2 font-mono text-[10.5px] text-[var(--color-foreground)]">
                  {pending.files.slice(0, MAX_FILES).map((file) => (
                    <li key={file} className="truncate">{file}</li>
                  ))}
                  {pending.files.length > MAX_FILES && (
                    <li className="text-[var(--color-muted-foreground)]">
                      {t('branches.confirmMore').replace('{count}', String(pending.files.length - MAX_FILES))}
                    </li>
                  )}
                </ul>
              )}
              {error && <div className="text-[11px] text-[var(--color-destructive)]">{error}</div>}
              <div className="flex flex-wrap gap-1.5">
                <button type="button" disabled={switching} onClick={() => void checkout(pending.branch, pending.create, 'stash')} className="rounded-[var(--radius-md)] bg-[var(--color-primary)] px-2 py-1 text-[11.5px] font-medium text-[var(--color-on-primary)] disabled:opacity-50">
                  {t('branches.confirmStash')}
                </button>
                <button type="button" disabled={switching} onClick={() => void checkout(pending.branch, pending.create, 'force')} className="rounded-[var(--radius-md)] bg-[var(--color-destructive)] px-2 py-1 text-[11.5px] font-medium text-[var(--color-on-destructive)] disabled:opacity-50">
                  {t('branches.confirmDiscard')}
                </button>
                <button type="button" disabled={switching} onClick={() => setPending(null)} className="rounded-[var(--radius-md)] border border-[var(--color-border)] px-2 py-1 text-[11.5px] text-[var(--color-foreground)] disabled:opacity-50">
                  {t('branches.confirmCancel')}
                </button>
              </div>
              <div className="text-[10.5px] leading-relaxed text-[var(--color-muted-foreground)]">{t('branches.confirmHint')}</div>
            </div>
          ) : (
            <>
              <div className="flex items-center gap-2 rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-background)] px-2 py-1.5">
                <MagnifyingGlassIcon className="h-3.5 w-3.5 text-[var(--color-muted-foreground)]" />
                <input
                  value={query}
                  onChange={(e) => setQuery(e.target.value)}
                  placeholder={t('branches.search')}
                  className="min-w-0 flex-1 border-none bg-transparent text-[12.5px] text-[var(--color-foreground)] outline-none"
                />
              </div>
              <div className="px-2 pb-1 pt-2 text-[10px] font-semibold uppercase tracking-[0.06em] text-[var(--color-muted-foreground)]">
                {t('branches.title')}
              </div>
              <div className="max-h-56 overflow-y-auto">
                {filtered.length === 0 ? (
                  <div className="px-2 py-3 text-xs text-[var(--color-muted-foreground)]">{t('branches.none')}</div>
                ) : (
                  filtered.map((branch) => (
                    <button
                      key={branch}
                      type="button"
                      disabled={switching}
                      onClick={() => void checkout(branch)}
                      className={`flex w-full items-center gap-2 rounded-[var(--radius-md)] px-2 py-1.5 text-left text-[12.5px] transition-colors hover:bg-[var(--color-muted)] disabled:opacity-55 ${
                        branch === current ? 'font-semibold text-[var(--color-primary)]' : 'text-[var(--color-foreground)]'
                      }`}
                    >
                      <CodeBracketIcon className="h-3.5 w-3.5 shrink-0" />
                      <span className="min-w-0 flex-1 truncate font-mono">{branch}</span>
                      {branch === current && <CheckIcon className="h-3.5 w-3.5 shrink-0" />}
                    </button>
                  ))
                )}
              </div>
              {error && <div className="px-2 py-1 text-[11px] text-[var(--color-destructive)]">{error}</div>}
              <div className="mt-1 border-t border-[var(--color-border)] pt-1">
                {!creating ? (
                  <button type="button" onClick={startCreate} className="flex w-full items-center gap-2 rounded-[var(--radius-md)] px-2 py-1.5 text-left text-xs text-[var(--color-foreground)] hover:bg-[var(--color-muted)]">
                    <PlusIcon className="h-3.5 w-3.5" />
                    {t('branches.create')}
                  </button>
                ) : (
                  <div className="flex gap-1.5">
                    <input
                      ref={inputRef}
                      value={newName}
                      onChange={(e) => setNewName(e.target.value)}
                      onKeyDown={(e) => {
                        if (e.key === 'Enter') void checkout(newName, true)
                        if (e.key === 'Escape') setCreating(false)
                      }}
                      placeholder={t('branches.newName')}
                      className="min-w-0 flex-1 rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-background)] px-2 py-1.5 text-xs text-[var(--color-foreground)] outline-none"
                    />
                    <button type="button" disabled={!newName.trim() || switching} onClick={() => void checkout(newName, true)} className="rounded-[var(--radius-md)] bg-[var(--color-primary)] px-2 py-1.5 text-xs font-medium text-[var(--color-on-primary)] disabled:opacity-50">
                      {t('branches.createBtn')}
                    </button>
                  </div>
                )}
              </div>
            </>
          )}
        </div>
      )}
    </div>
  )
}
