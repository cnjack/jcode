/**
 * SidebarFilterMenu — drill-down filter/sort popover for the sidebar tree.
 *
 * Ported from web/src/components/SidebarFilterMenu.vue: a compact root list of
 * filter rows (label + current value + chevron), each of which drills into a
 * flat option list with a back button. Keeps the panel anchored to the narrow
 * rail instead of a side-flyout.
 */

import { useEffect, useMemo, useRef, useState } from 'react'
import {
  AdjustmentsHorizontalIcon,
  ArrowPathIcon,
  CheckIcon,
  ChevronLeftIcon,
  ChevronRightIcon,
} from '@heroicons/react/24/outline'
import { useTranslation } from 'react-i18next'

export type StatusFilter = 'all' | 'active' | 'archived'
export type LastActivityFilter = 'all' | 'today' | 'week' | 'month'
export type GroupByMode = 'project' | 'date'
export type SortFilter = 'recency' | 'name' | 'created'

export interface FilterState {
  status: StatusFilter
  project: string
  lastActivity: LastActivityFilter
  groupBy: GroupByMode
  sort: SortFilter
}

export const DEFAULT_FILTERS: FilterState = {
  status: 'active',
  project: '',
  lastActivity: 'all',
  groupBy: 'project',
  sort: 'recency',
}

type RowKey = keyof FilterState

interface Opt {
  value: string
  label: string
}

interface Row {
  key: RowKey
  label: string
  options: Opt[]
}

interface Props {
  filters: FilterState
  projects: { path: string; name: string }[]
  onChange: (next: FilterState | ((prev: FilterState) => FilterState)) => void
}

export function SidebarFilterMenu({ filters, projects, onChange }: Props) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [openSub, setOpenSub] = useState<RowKey | ''>('')
  const rootRef = useRef<HTMLDivElement | null>(null)

  const filterRows = useMemo<Row[]>(
    () => [
      {
        key: 'status',
        label: t('sidebar.filter.status'),
        options: [
          { value: 'active', label: t('sidebar.filter.statusActive') },
          { value: 'archived', label: t('sidebar.filter.statusArchived') },
          { value: 'all', label: t('sidebar.filter.statusAll') },
        ],
      },
      {
        key: 'project',
        label: t('sidebar.filter.project'),
        options: [
          { value: '', label: t('sidebar.filter.projectAll') },
          ...projects.map((p) => ({ value: p.path, label: p.name })),
        ],
      },
      {
        key: 'lastActivity',
        label: t('sidebar.filter.lastActivity'),
        options: [
          { value: 'all', label: t('sidebar.filter.activityAll') },
          { value: 'today', label: t('sidebar.filter.activityToday') },
          { value: 'week', label: t('sidebar.filter.activityWeek') },
          { value: 'month', label: t('sidebar.filter.activityMonth') },
        ],
      },
    ],
    [projects, t],
  )

  const viewRows = useMemo<Row[]>(
    () => [
      {
        key: 'groupBy',
        label: t('sidebar.filter.groupBy'),
        options: [
          { value: 'project', label: t('sidebar.filter.groupProject') },
          { value: 'date', label: t('sidebar.filter.groupDate') },
        ],
      },
      {
        key: 'sort',
        label: t('sidebar.filter.sortBy'),
        options: [
          { value: 'recency', label: t('sidebar.filter.sortRecency') },
          { value: 'name', label: t('sidebar.filter.sortName') },
          { value: 'created', label: t('sidebar.filter.sortCreated') },
        ],
      },
    ],
    [t],
  )

  const allRows = useMemo(() => [...filterRows, ...viewRows], [filterRows, viewRows])
  const activeRow = openSub ? allRows.find((r) => r.key === openSub) ?? null : null

  const isDirty = (Object.keys(DEFAULT_FILTERS) as RowKey[]).some(
    (k) => filters[k] !== DEFAULT_FILTERS[k],
  )

  function currentLabel(row: Row): string {
    const cur = String(filters[row.key])
    const match = row.options.find((o) => o.value === cur)
    return match ? match.label : row.options[0]?.label ?? ''
  }

  function isSelected(row: Row, value: string): boolean {
    return String(filters[row.key]) === value
  }

  function apply(key: RowKey, value: string) {
    onChange((prev) => ({ ...prev, [key]: value }))
    setOpenSub('')
  }

  function reset() {
    onChange({ ...DEFAULT_FILTERS })
    setOpenSub('')
  }

  function toggle() {
    setOpen((v) => {
      const next = !v
      if (next) setOpenSub('')
      return next
    })
  }

  function close() {
    setOpen(false)
    setOpenSub('')
  }

  useEffect(() => {
    if (!open) return
    function onDocClick(e: MouseEvent) {
      if (rootRef.current && !rootRef.current.contains(e.target as Node)) close()
    }
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') {
        e.stopPropagation()
        close()
      }
    }
    document.addEventListener('click', onDocClick)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('click', onDocClick)
      document.removeEventListener('keydown', onKey)
    }
  }, [open])

  return (
    <div ref={rootRef} className="fm-root relative inline-flex">
      <button
        type="button"
        className={`fm-btn relative grid h-[22px] w-[22px] place-items-center rounded-[var(--radius-sm)] border-none bg-transparent text-[var(--color-muted-foreground)] transition-[background,color] duration-150 ${
          open || isDirty ? 'text-[var(--color-foreground)]' : ''
        } ${open ? 'bg-[var(--color-muted)]' : ''} hover:bg-[var(--color-muted)] hover:text-[var(--color-foreground)]`}
        title={t('sidebar.filter.title')}
        aria-label={t('sidebar.filter.title')}
        aria-expanded={open}
        onClick={(e) => {
          e.stopPropagation()
          toggle()
        }}
      >
        <AdjustmentsHorizontalIcon className="h-4 w-4" />
        {isDirty && (
          <span
            className="absolute right-px top-px h-[5px] w-[5px] rounded-[var(--radius-pill)] bg-[var(--color-accent-neutral)]"
            aria-hidden="true"
          />
        )}
      </button>

      {open && (
        <div
          className="fm-panel sb-pop absolute right-0 top-[calc(100%+4px)] z-[var(--z-dropdown)] max-h-[380px] w-[232px] overflow-y-auto rounded-[var(--radius-lg)] border border-[var(--color-border)] bg-[var(--color-surface)] p-1 shadow-[var(--shadow-md)] outline-none"
          role="menu"
          onClick={(e) => e.stopPropagation()}
        >
          {!activeRow ? (
            <>
              {filterRows.map((row) => (
                <FilterRootRow
                  key={row.key}
                  label={row.label}
                  value={currentLabel(row)}
                  onClick={() => setOpenSub(row.key)}
                />
              ))}
              <div className="fm-sep my-1 h-px bg-[var(--color-border)]" />
              {viewRows.map((row) => (
                <FilterRootRow
                  key={row.key}
                  label={row.label}
                  value={currentLabel(row)}
                  onClick={() => setOpenSub(row.key)}
                />
              ))}
              {isDirty && (
                <>
                  <div className="fm-sep my-1 h-px bg-[var(--color-border)]" />
                  <button
                    type="button"
                    className="fm-row flex w-full items-center gap-2 rounded-[var(--radius-md)] border-none bg-transparent px-2 py-[7px] text-left text-[12.5px] text-[var(--color-muted-foreground)] transition-colors hover:bg-[var(--color-muted)]"
                    onClick={reset}
                  >
                    <ArrowPathIcon className="h-3.5 w-3.5 shrink-0" />
                    <span>{t('sidebar.filter.reset')}</span>
                  </button>
                </>
              )}
            </>
          ) : (
            <>
              <button
                type="button"
                className="fm-back flex w-full items-center gap-1.5 rounded-[var(--radius-md)] border-none bg-transparent px-2 py-1.5 text-left text-[12.5px] font-medium text-[var(--color-foreground)] transition-colors hover:bg-[var(--color-muted)]"
                onClick={() => setOpenSub('')}
              >
                <ChevronLeftIcon className="h-3.5 w-3.5 shrink-0" />
                <span>{activeRow.label}</span>
              </button>
              <div className="fm-sep my-1 h-px bg-[var(--color-border)]" />
              <div className="flex flex-col">
                {activeRow.options.map((opt) => {
                  const selected = isSelected(activeRow, opt.value)
                  return (
                    <button
                      key={opt.value || '__all__'}
                      type="button"
                      className={`fm-opt flex w-full items-center gap-2 rounded-[var(--radius-md)] border-none bg-transparent px-2 py-[7px] text-left text-[12.5px] transition-colors hover:bg-[var(--color-muted)] ${
                        selected
                          ? 'text-[var(--color-accent-neutral)]'
                          : 'text-[var(--color-foreground)]'
                      }`}
                      onClick={() => apply(activeRow.key, opt.value)}
                    >
                      <span className="min-w-0 flex-1 truncate">{opt.label}</span>
                      {selected && (
                        <CheckIcon className="h-3.5 w-3.5 shrink-0 text-[var(--color-accent-neutral)]" />
                      )}
                    </button>
                  )
                })}
              </div>
            </>
          )}
        </div>
      )}
    </div>
  )
}

function FilterRootRow({
  label,
  value,
  onClick,
}: {
  label: string
  value: string
  onClick: () => void
}) {
  return (
    <button
      type="button"
      className="fm-row flex w-full items-center gap-2 rounded-[var(--radius-md)] border-none bg-transparent px-2 py-[7px] text-left text-[12.5px] text-[var(--color-foreground)] transition-colors hover:bg-[var(--color-muted)]"
      onClick={onClick}
    >
      <span className="shrink-0">{label}</span>
      <span className="min-w-0 flex-1 truncate text-right text-[var(--color-muted-foreground)]">
        {value}
      </span>
      <ChevronRightIcon className="h-3.5 w-3.5 shrink-0 text-[var(--color-muted-foreground)]" />
    </button>
  )
}
