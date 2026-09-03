/**
 * AutomationsView — scheduled/manual agent runs.
 *
 * Ported from web/src/components/AutomationsView.vue +
 * AutomationEditorDialog.vue. The editor is rendered inline as a modal panel
 * (the React app has no separate AutomationEditorDialog component). Features:
 *   - Automation list: cards with name, cadence chip, status chip, schedule,
 *     next/last run, edit/delete/enable-toggle/run-now actions.
 *   - Create/edit form (modal): name, trigger (manual/hourly/daily/weekly),
 *     day/hour/minute, mode, project path, prompt.
 *   - Run history: filter (all/success/failed) + search + clickable rows.
 *   - Templates: pick a template to prefill the editor.
 *
 * No Redux — the view is self-contained and fetches via the api client.
 */

import { useEffect, useMemo, useState, type FormEvent } from 'react'
import { useTranslation, Trans } from 'react-i18next'
import type { TFunction } from 'i18next'
import {
  BoltIcon,
  SparklesIcon,
  PlusIcon,
  PlayIcon,
  TrashIcon,
  PencilIcon,
  ClockIcon,
  XMarkIcon,
  CheckCircleIcon,
  ExclamationCircleIcon,
  CalendarDaysIcon,
  HandRaisedIcon,
  DocumentDuplicateIcon,
} from '@heroicons/react/24/outline'
import { api } from '../lib/api'
import { toDatetimeLocal, toLocalRFC3339, triggerKindLabel } from '../lib/automation'
import type {
  Automation,
  AutomationCadence,
  AutomationCreate,
  AutomationItem,
  AutomationRun,
  AutomationTemplate,
  AutomationTrigger,
} from '../lib/automation'

// ─── Local helpers ───────────────────────────────────────────────────────────

type View = 'list' | 'templates'
type StatusFilter = 'all' | 'success' | 'failed'
type CardState = 'running' | 'error' | 'success' | 'paused'
type FormTrigger = 'manual' | 'hourly' | 'daily' | 'weekly' | 'once' | 'cron'

const WEEKDAY_KEYS = ['sun', 'mon', 'tue', 'wed', 'thu', 'fri', 'sat'] as const

function cardState(a: AutomationItem): CardState {
  if (!a.enabled) return 'paused'
  const s = a.state.last_status
  if (s === 'running') return 'running'
  if (s === 'error') return 'error'
  return 'success'
}

function isRunning(a: AutomationItem): boolean {
  return a.state.last_status === 'running'
}

/** Compact relative time: "just now" / "3m" / "6h 12m" / "3d". */
function relLabel(diff: number, t: TFunction): string {
  const abs = Math.abs(diff)
  const min = 60_000
  const hr = 60 * min
  const day = 24 * hr
  if (abs < min) return t('automations.rel.justNow')
  if (abs < hr) return t('automations.rel.minutes', { n: Math.round(abs / min) })
  if (abs < day) {
    const h = Math.floor(abs / hr)
    const m = Math.round((abs % hr) / min)
    return m ? t('automations.rel.hoursMinutes', { h, m }) : t('automations.rel.hours', { h })
  }
  return t('automations.rel.days', { n: Math.round(abs / day) })
}

function relTime(iso: string, t: TFunction): string {
  return relLabel(Date.now() - new Date(iso).getTime(), t)
}

function relTimeFromNow(iso: string, t: TFunction): string {
  return relLabel(new Date(iso).getTime() - Date.now(), t)
}

function nextRunLabel(a: AutomationItem, t: TFunction): string {
  if (a.trigger.type === 'manual') {
    return lastRunLabel(a, t)
  }
  if (a.trigger.type === 'once') {
    // Prefer the scheduler's seeded slot; fall back to the pinned time while
    // it is still future (unseeded first 30s, or due-but-not-yet-fired), and
    // only show the last-run outcome once the pin itself is past.
    const next = a.state.next_run_at
    if (next && new Date(next).getTime() > Date.now()) {
      return t('automations.nextRunIn', { time: relTimeFromNow(next, t) })
    }
    const at = a.trigger.at
    if (a.enabled && at && new Date(at).getTime() > Date.now()) {
      return t('automations.nextRunIn', { time: relTimeFromNow(at, t) })
    }
    return lastRunLabel(a, t)
  }
  const next = a.state.next_run_at
  if (!next || new Date(next).getTime() <= Date.now()) {
    // Overdue slot awaiting the next tick — a relative "in {time}" would be
    // wrong, so fall back to the timeless schedule string.
    return a.human_schedule
  }
  return t('automations.nextRunIn', { time: relTimeFromNow(next, t) })
}

function lastRunLabel(a: AutomationItem, t: TFunction): string {
  const last = a.state.last_run_at
  if (!last) return t('automations.notRunYet')
  const rel = relTime(last, t)
  if (a.state.last_status === 'success') return t('automations.lastRunOk', { time: rel })
  if (a.state.last_status === 'error') return t('automations.lastRunFailed', { time: rel })
  return rel
}

function runLabel(r: AutomationRun): string {
  return r.start_time ? new Date(r.start_time).toLocaleString() : ''
}

/** Whether the meta row shows "next run" (future fire) or falls back to "last run". */
function usesNextRun(a: AutomationItem): boolean {
  if (a.trigger.type === 'manual') return false
  if (a.trigger.type === 'once') {
    const next = a.state.next_run_at
    if (next && new Date(next).getTime() > Date.now()) return true
    const at = a.trigger.at
    return a.enabled && !!at && new Date(at).getTime() > Date.now()
  }
  return true
}

function modeLabel(mode: string | undefined, t: TFunction): string {
  if (mode === 'approval') return t('automations.mode.ask')
  if (mode === 'plan') return t('automations.mode.plan')
  if (mode === 'auto') return t('automations.mode.auto')
  if (mode === 'full_access') return t('automations.mode.autopilot')
  return mode || t('automations.mode.autopilot')
}

// ─── Cadence chip ────────────────────────────────────────────────────────────

function CadenceChip({ a }: { a: AutomationItem | AutomationTemplate }) {
  const { t } = useTranslation()
  const trig: AutomationTrigger = a.trigger
  let label = t('automations.cadence.schedule')
  let Icon = CalendarDaysIcon
  if (trig.type === 'manual') {
    label = t('automations.cadence.manual')
    Icon = HandRaisedIcon
  } else if (trig.type === 'once') {
    label = t('automations.cadence.once')
    Icon = ClockIcon
  } else if (trig.cadence === 'hourly') {
    label = t('automations.cadence.hourly')
    Icon = ClockIcon
  } else if (trig.cadence === 'daily') {
    label = t('automations.cadence.daily')
    Icon = CalendarDaysIcon
  } else if (trig.cadence === 'weekly') {
    label = t('automations.cadence.weekly')
    Icon = CalendarDaysIcon
  } else if (trig.cadence === 'cron') {
    label = t('automations.cadence.cron')
    Icon = CalendarDaysIcon
  }
  return (
    <span className="inline-flex flex-shrink-0 items-center gap-1 whitespace-nowrap rounded-full bg-[var(--color-muted)] px-2 py-[3px] text-[11px] font-medium leading-none text-[var(--color-muted-foreground)]">
      <Icon className="h-3 w-3" />
      {label}
    </span>
  )
}

// ─── Card status chip ────────────────────────────────────────────────────────

function StatusChip({ state }: { state: CardState }) {
  const { t } = useTranslation()
  if (state === 'success') {
    return (
      <span className="inline-flex flex-shrink-0 items-center gap-1 whitespace-nowrap rounded-full bg-[var(--color-success-bg)] px-[7px] py-[3px] text-[10.5px] font-semibold leading-none text-[var(--color-success-fg)]">
        <CheckCircleIcon className="h-3 w-3" />
        {t('automations.status.ranOk')}
      </span>
    )
  }
  if (state === 'running') {
    return (
      <span className="inline-flex flex-shrink-0 items-center gap-1 whitespace-nowrap rounded-full bg-[var(--accent-wash)] px-[7px] py-[3px] text-[10.5px] font-semibold leading-none text-[var(--color-primary)]">
        <PlayIcon className="h-3 w-3" />
        {t('automations.status.running')}
      </span>
    )
  }
  return (
    <span className="inline-flex flex-shrink-0 items-center gap-1 whitespace-nowrap rounded-full bg-[var(--color-error-bg)] px-[7px] py-[3px] text-[10.5px] font-semibold leading-none text-[var(--color-error-fg)]">
      <ExclamationCircleIcon className="h-3 w-3" />
      {t('automations.status.failed')}
    </span>
  )
}

// ─── Toggle switch ───────────────────────────────────────────────────────────

function Toggle({
  checked,
  onChange,
  title,
}: {
  checked: boolean
  onChange: (next: boolean) => void
  title?: string
}) {
  return (
    <label title={title} className="inline-flex cursor-pointer">
      <input
        type="checkbox"
        checked={checked}
        onChange={(e) => onChange(e.target.checked)}
        className="hidden"
      />
      <span
        className="relative h-[18px] w-8 rounded-full transition-colors"
        style={{ background: checked ? 'var(--color-primary)' : 'var(--color-muted)' }}
      >
        <span
          className="absolute top-[2px] left-[2px] h-[14px] w-[14px] rounded-full bg-[var(--color-surface)] shadow-[var(--shadow-sm)] transition-transform"
          style={{ transform: checked ? 'translateX(14px)' : 'translateX(0)' }}
        />
      </span>
    </label>
  )
}

// ─── Editor modal ────────────────────────────────────────────────────────────

interface EditorState {
  editing: AutomationItem | null
  prefill: Partial<AutomationCreate> | null
}

interface FormValues {
  name: string
  trigger: FormTrigger
  hour: number
  minute: number
  weekday: number
  /** 5-field cron expression (trigger === 'cron'). */
  cronExpr: string
  /** datetime-local input value (trigger === 'once'). */
  onceAt: string
  projectPath: string
  mode: string
  prompt: string
  enabled: boolean
}

/** Fill trigger-related form fields from a trigger value (mutates + returns base). */
function applyTrigger(trig: AutomationTrigger, base: FormValues): FormValues {
  if (trig.type === 'manual') {
    base.trigger = 'manual'
  } else if (trig.type === 'once') {
    base.trigger = 'once'
    base.onceAt = toDatetimeLocal(trig.at)
  } else if (trig.cadence === 'cron') {
    base.trigger = 'cron'
    base.cronExpr = trig.expr || ''
  } else {
    base.trigger = (trig.cadence as FormTrigger) || 'daily'
    base.hour = trig.hour ?? 9
    base.minute = trig.minute ?? 0
    base.weekday = trig.weekday ?? 1
  }
  return base
}

function buildForm(editing: AutomationItem | null, prefill: Partial<AutomationCreate> | null): FormValues {
  const blank: FormValues = {
    name: '',
    trigger: 'daily',
    hour: 9,
    minute: 0,
    weekday: 1,
    cronExpr: '',
    onceAt: '',
    projectPath: '',
    mode: 'full_access',
    prompt: '',
    enabled: true,
  }
  if (editing) {
    return applyTrigger(editing.trigger, {
      ...blank,
      name: editing.name,
      projectPath: editing.project_path,
      mode: editing.mode || 'full_access',
      prompt: editing.prompt,
      enabled: editing.enabled,
    })
  }
  if (prefill) {
    if (prefill.name) blank.name = prefill.name
    if (prefill.prompt) blank.prompt = prefill.prompt
    if (prefill.mode) blank.mode = prefill.mode
    if (prefill.project_path) blank.projectPath = prefill.project_path
    if (prefill.trigger) applyTrigger(prefill.trigger, blank)
  }
  return blank
}

function buildPayload(form: FormValues, runNow: boolean): AutomationCreate {
  let trigger: AutomationTrigger
  let mode = form.mode
  switch (form.trigger) {
    case 'manual':
      trigger = { type: 'manual' }
      break
    case 'once': {
      const at = form.onceAt ? new Date(form.onceAt) : null
      trigger = { type: 'once', at: at && !Number.isNaN(at.getTime()) ? toLocalRFC3339(at) : undefined }
      break
    }
    case 'cron':
      trigger = { type: 'schedule', cadence: 'cron', expr: form.cronExpr.trim() }
      break
    default:
      trigger = {
        type: 'schedule',
        cadence: form.trigger as AutomationCadence,
        hour: form.hour,
        minute: form.minute,
        weekday: form.weekday,
      }
  }
  // A scheduled automation runs unattended, so it must auto-approve.
  if (form.trigger !== 'manual') mode = 'full_access'
  return {
    name: form.name.trim(),
    prompt: form.prompt.trim(),
    trigger,
    project_path: form.projectPath.trim(),
    mode,
    enabled: form.enabled,
    run_now: runNow,
  }
}

function AutomationEditor({
  state,
  onClose,
  onSaved,
}: {
  state: EditorState
  onClose: () => void
  onSaved: () => void
}) {
  const { t } = useTranslation()
  const editing = state.editing
  const [form, setForm] = useState<FormValues>(() => buildForm(editing, state.prefill))
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  // Reset the form whenever the editor is (re)opened for a different target.
  useEffect(() => {
    setForm(buildForm(editing, state.prefill))
    setError(null)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [editing, state.prefill])

  const isSchedule = form.trigger !== 'manual'
  const showHour = form.trigger === 'daily' || form.trigger === 'weekly'
  const showWeekday = form.trigger === 'weekly'
  const showMinute = isSchedule && form.trigger !== 'once' && form.trigger !== 'cron'
  const showOnceAt = form.trigger === 'once'
  const showCronExpr = form.trigger === 'cron'

  function update<K extends keyof FormValues>(key: K, value: FormValues[K]) {
    setForm((f) => ({ ...f, [key]: value }))
  }

  async function save(runNow: boolean) {
    setError(null)
    if (!form.name.trim()) {
      setError(t('automations.editor.errName'))
      return
    }
    if (!form.prompt.trim()) {
      setError(t('automations.editor.errPrompt'))
      return
    }
    if (!form.projectPath.trim()) {
      setError(t('automations.editor.errProject'))
      return
    }
    if (form.trigger === 'once' && !form.onceAt) {
      setError(t('automations.editor.errOnceAt'))
      return
    }
    // New one-shots must not point before the current minute (the API's gate);
    // editing an expired one stays allowed so its record is fixable.
    if (!editing && form.trigger === 'once' && form.onceAt) {
      const minuteFloor = new Date()
      minuteFloor.setSeconds(0, 0)
      if (new Date(form.onceAt).getTime() < minuteFloor.getTime()) {
        setError(t('automations.editor.errOnceAtPast'))
        return
      }
    }
    if (form.trigger === 'cron' && !form.cronExpr.trim()) {
      setError(t('automations.editor.errCronExpr'))
      return
    }
    setSaving(true)
    try {
      const payload = buildPayload(form, runNow)
      if (editing) {
        await api.automationUpdate(editing.id, payload as Partial<Automation>)
      } else {
        await api.automationCreate(payload)
      }
      onSaved()
      onClose()
    } catch (e) {
      setError(e instanceof Error ? e.message : t('automations.editor.errSave'))
    } finally {
      setSaving(false)
    }
  }

  function onSubmit(e: FormEvent) {
    e.preventDefault()
    void save(false)
  }

  return (
    <div
      className="fixed inset-0 z-[var(--z-modal)] grid place-items-center p-6"
      style={{ background: 'color-mix(in srgb, var(--color-background) 55%, transparent)', backdropFilter: 'blur(3px)' }}
      onClick={onClose}
    >
      <form
        noValidate
        onClick={(e) => e.stopPropagation()}
        onSubmit={onSubmit}
        role="dialog"
        aria-modal="true"
        className="flex max-h-[88vh] w-full max-w-[560px] flex-col rounded-[var(--radius-2xl)] border border-[var(--color-border)] bg-[var(--color-surface)] shadow-[var(--shadow-lg)]"
      >
        <header className="flex items-center justify-between px-5 pb-3 pt-[18px]">
          <h2 className="text-base font-semibold text-[var(--color-foreground)]">
            {editing ? t('automations.editor.editTitle') : t('automations.new')}
          </h2>
          <button
            type="button"
            aria-label={t('common.close')}
            onClick={onClose}
            className="cursor-pointer rounded-lg p-1 text-[var(--color-muted-foreground)] hover:bg-[var(--color-muted)]"
          >
            <XMarkIcon className="h-5 w-5" />
          </button>
        </header>

        <div className="flex flex-col gap-[14px] px-5 pb-4">
          <label className="flex flex-1 flex-col gap-1.5">
            <span className="text-xs font-semibold text-[var(--color-foreground)]">{t('automations.editor.name')}</span>
            <input
              value={form.name}
              onChange={(e) => update('name', e.target.value)}
              placeholder={t('automations.editor.namePlaceholder')}
              className="w-full rounded-[var(--radius-lg)] border border-[var(--color-border)] bg-[var(--color-background)] px-2.5 py-2 text-[13px] text-[var(--color-foreground)] outline-none focus:border-[var(--color-primary)]"
            />
          </label>

          <div className="flex flex-wrap gap-2.5">
            <label className="flex flex-1 min-w-0 flex-col gap-1.5">
              <span className="text-xs font-semibold text-[var(--color-foreground)]">{t('automations.editor.trigger')}</span>
              <select
                value={form.trigger}
                onChange={(e) => update('trigger', e.target.value as FormTrigger)}
                className="w-full rounded-[var(--radius-lg)] border border-[var(--color-border)] bg-[var(--color-background)] px-2.5 py-2 text-[13px] text-[var(--color-foreground)] outline-none focus:border-[var(--color-primary)]"
              >
                <option value="daily">{t('automations.cadence.daily')}</option>
                <option value="weekly">{t('automations.cadence.weekly')}</option>
                <option value="hourly">{t('automations.cadence.hourly')}</option>
                <option value="cron">{t('automations.cadence.cron')}</option>
                <option value="once">{t('automations.cadence.once')}</option>
                <option value="manual">{t('automations.cadence.manual')}</option>
              </select>
            </label>

            {showOnceAt && (
              <label className="flex flex-1 min-w-0 flex-col gap-1.5">
                <span className="text-xs font-semibold text-[var(--color-foreground)]">{t('automations.editor.onceAt')}</span>
                <input
                  type="datetime-local"
                  min={toDatetimeLocal(new Date().toISOString())}
                  value={form.onceAt}
                  onChange={(e) => update('onceAt', e.target.value)}
                  className="w-full rounded-[var(--radius-lg)] border border-[var(--color-border)] bg-[var(--color-background)] px-2.5 py-2 text-[13px] text-[var(--color-foreground)] outline-none focus:border-[var(--color-primary)]"
                />
              </label>
            )}

            {showCronExpr && (
              <label className="flex flex-1 min-w-0 flex-col gap-1.5">
                <span className="text-xs font-semibold text-[var(--color-foreground)]">{t('automations.cadence.cron')}</span>
                <input
                  value={form.cronExpr}
                  onChange={(e) => update('cronExpr', e.target.value)}
                  placeholder="*/15 * * * *"
                  className="w-full rounded-[var(--radius-lg)] border border-[var(--color-border)] bg-[var(--color-background)] px-2.5 py-2 font-mono text-[13px] text-[var(--color-foreground)] outline-none focus:border-[var(--color-primary)]"
                />
                <span className="text-[11px] text-[var(--color-muted-foreground)]">
                  {t('automations.editor.cronHint')}
                </span>
              </label>
            )}

            {showWeekday && (
              <label className="flex flex-1 min-w-0 flex-col gap-1.5">
                <span className="text-xs font-semibold text-[var(--color-foreground)]">{t('automations.editor.day')}</span>
                <select
                  value={form.weekday}
                  onChange={(e) => update('weekday', Number(e.target.value))}
                  className="w-full rounded-[var(--radius-lg)] border border-[var(--color-border)] bg-[var(--color-background)] px-2.5 py-2 text-[13px] text-[var(--color-foreground)] outline-none focus:border-[var(--color-primary)]"
                >
                  {WEEKDAY_KEYS.map((key, i) => (
                    <option key={key} value={i}>
                      {t(`automations.weekday.${key}`)}
                    </option>
                  ))}
                </select>
              </label>
            )}

            {showHour && (
              <label className="flex flex-1 min-w-0 flex-col gap-1.5">
                <span className="text-xs font-semibold text-[var(--color-foreground)]">{t('automations.editor.hour')}</span>
                <select
                  value={form.hour}
                  onChange={(e) => update('hour', Number(e.target.value))}
                  className="w-full rounded-[var(--radius-lg)] border border-[var(--color-border)] bg-[var(--color-background)] px-2.5 py-2 text-[13px] text-[var(--color-foreground)] outline-none focus:border-[var(--color-primary)]"
                >
                  {Array.from({ length: 24 }, (_, h) => (
                    <option key={h} value={h}>
                      {String(h).padStart(2, '0')}
                    </option>
                  ))}
                </select>
              </label>
            )}

            {showMinute && (
              <label className="flex flex-1 min-w-0 flex-col gap-1.5">
                <span className="text-xs font-semibold text-[var(--color-foreground)]">{t('automations.editor.minute')}</span>
                <select
                  value={form.minute}
                  onChange={(e) => update('minute', Number(e.target.value))}
                  className="w-full rounded-[var(--radius-lg)] border border-[var(--color-border)] bg-[var(--color-background)] px-2.5 py-2 text-[13px] text-[var(--color-foreground)] outline-none focus:border-[var(--color-primary)]"
                >
                  {Array.from({ length: 60 }, (_, m) => (
                    <option key={m} value={m}>
                      :{String(m).padStart(2, '0')}
                    </option>
                  ))}
                </select>
              </label>
            )}
          </div>

          <label className="flex flex-1 flex-col gap-1.5">
            <span className="text-xs font-semibold text-[var(--color-foreground)]">{t('automations.editor.project')}</span>
            <input
              value={form.projectPath}
              onChange={(e) => update('projectPath', e.target.value)}
              placeholder={t('automations.editor.projectPlaceholder')}
              className="w-full rounded-[var(--radius-lg)] border border-[var(--color-border)] bg-[var(--color-background)] px-2.5 py-2 text-[13px] text-[var(--color-foreground)] outline-none focus:border-[var(--color-primary)]"
            />
            <span className="text-[11px] text-[var(--color-muted-foreground)]">
              {t('automations.editor.projectHint')}
            </span>
          </label>

          {!isSchedule ? (
            <label className="flex flex-1 flex-col gap-1.5">
              <span className="text-xs font-semibold text-[var(--color-foreground)]">{t('automations.editor.mode')}</span>
              <select
                value={form.mode}
                onChange={(e) => update('mode', e.target.value)}
                className="w-full rounded-[var(--radius-lg)] border border-[var(--color-border)] bg-[var(--color-background)] px-2.5 py-2 text-[13px] text-[var(--color-foreground)] outline-none focus:border-[var(--color-primary)]"
              >
                <option value="full_access">{t('automations.mode.autopilot')}</option>
                <option value="approval">{t('automations.mode.ask')}</option>
                <option value="plan">{t('automations.mode.plan')}</option>
              </select>
            </label>
          ) : (
            <p className="text-xs text-[var(--color-muted-foreground)]">
              <Trans
                i18nKey="automations.editor.scheduleModeNote"
                components={{ b: <strong className="font-semibold" /> }}
              />
            </p>
          )}

          <label className="flex flex-1 flex-col gap-1.5">
            <span className="text-xs font-semibold text-[var(--color-foreground)]">{t('automations.editor.prompt')}</span>
            <textarea
              value={form.prompt}
              onChange={(e) => update('prompt', e.target.value)}
              rows={4}
              placeholder={t('automations.editor.promptPlaceholder')}
              className="w-full resize-y rounded-[var(--radius-lg)] border border-[var(--color-border)] bg-[var(--color-background)] px-2.5 py-2 font-sans text-[13px] leading-relaxed text-[var(--color-foreground)] outline-none focus:border-[var(--color-primary)]"
            />
          </label>

          {error && <p className="text-xs text-[var(--color-destructive)]">{error}</p>}
        </div>

        <footer className="flex items-center justify-between gap-3 rounded-b-[var(--radius-2xl)] border-t border-[var(--color-border)] bg-[var(--color-muted)] px-5 py-3">
          <span className="min-w-0 flex-1 text-[11px] text-[var(--color-muted-foreground)]">
            {t('automations.editor.footerHint')}
          </span>
          <div className="flex flex-shrink-0 gap-2">
            <button
              type="button"
              onClick={onClose}
              className="cursor-pointer whitespace-nowrap rounded-[var(--radius-lg)] border border-[var(--color-border)] bg-[var(--color-surface)] px-3 py-1.5 text-[12.5px] transition-colors hover:bg-[var(--color-muted)] disabled:cursor-not-allowed disabled:opacity-55"
            >
              {t('common.cancel')}
            </button>
            {!editing && (
              <button
                type="button"
                disabled={saving}
                onClick={() => void save(true)}
                className="cursor-pointer whitespace-nowrap rounded-[var(--radius-lg)] border border-[var(--color-border)] bg-[var(--color-surface)] px-3 py-1.5 text-[12.5px] transition-colors hover:bg-[var(--color-muted)] disabled:cursor-not-allowed disabled:opacity-55"
              >
                {t('automations.editor.createAndRun')}
              </button>
            )}
            <button
              type="submit"
              disabled={saving}
              className="cursor-pointer whitespace-nowrap rounded-[var(--radius-lg)] bg-[var(--color-primary)] px-3.5 py-1.5 text-[12.5px] font-medium text-[var(--color-on-primary)] transition-opacity hover:opacity-90 disabled:cursor-not-allowed disabled:opacity-55"
            >
              {editing ? t('common.save') : t('automations.editor.create')}
            </button>
          </div>
        </footer>
      </form>
    </div>
  )
}

// ─── Main view ───────────────────────────────────────────────────────────────

export function AutomationsView({ onOpenRun }: { onOpenRun?: (run: AutomationRun) => void }) {
  const { t } = useTranslation()
  const [items, setItems] = useState<AutomationItem[]>([])
  const [runs, setRuns] = useState<AutomationRun[]>([])
  const [templates, setTemplates] = useState<AutomationTemplate[]>([])
  const [loading, setLoading] = useState(true)

  const [view, setView] = useState<View>('list')
  const [statusFilter, setStatusFilter] = useState<StatusFilter>('all')
  const [search, setSearch] = useState('')

  const [editor, setEditor] = useState<EditorState | null>(null)

  async function fetchAll() {
    const [autoList, runList] = await Promise.all([api.automations(), api.automationRuns()])
    setItems(autoList)
    setRuns(runList)
  }

  useEffect(() => {
    let cancelled = false
    Promise.all([api.automations(), api.automationRuns(), api.automationTemplates()])
      .then(([autoList, runList, tplList]) => {
        if (cancelled) return
        setItems(autoList)
        setRuns(runList)
        setTemplates(tplList)
      })
      .catch(() => {})
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [])

  const filteredRuns = useMemo(() => {
    const q = search.trim().toLowerCase()
    return runs.filter((r) => {
      if (statusFilter === 'success' && r.terminal_status !== 'success') return false
      if (statusFilter === 'failed' && r.terminal_status !== 'error') return false
      if (q && !r.title.toLowerCase().includes(q)) return false
      return true
    })
  }, [runs, statusFilter, search])

  // ── Mutations (refetch after each so cards/runs reflect server state) ──
  function newAutomation() {
    setEditor({ editing: null, prefill: null })
  }
  function editAutomation(item: AutomationItem) {
    setEditor({ editing: item, prefill: null })
  }
  function fromTemplate(tpl: AutomationTemplate) {
    setEditor({
      editing: null,
      prefill: { name: tpl.name, prompt: tpl.prompt, trigger: tpl.trigger, mode: tpl.suggest_mode },
    })
    setView('list')
  }
  async function remove(id: string) {
    await api.automationDelete(id).catch(() => {})
    await fetchAll()
  }
  async function setEnabled(a: AutomationItem, enabled: boolean) {
    await api.automationUpdate(a.id, { enabled }).catch(() => {})
    await fetchAll()
  }
  async function runNow(id: string) {
    await api.automationRunNow(id).catch(() => {})
    await fetchAll()
  }

  return (
    <div className="page-surface flex min-h-0 flex-1 flex-col">
      <header className="flex h-[var(--header-height)] shrink-0 items-center gap-2 border-b border-[var(--color-border)] bg-[var(--color-surface)] px-4">
        <SparklesIcon className="h-4 w-4 text-[var(--color-primary)]" />
        <h1 className="flex-1 text-sm font-medium">{t('nav.automations')}</h1>
        <div className="flex gap-0.5 rounded-full bg-[var(--color-muted)] p-0.5">
          <button
            type="button"
            onClick={() => setView('list')}
            className={
              'rounded-full px-2.5 py-1 text-xs transition-colors ' +
              (view === 'list'
                ? 'bg-[var(--color-surface)] text-[var(--color-foreground)] shadow-[var(--shadow-sm)]'
                : 'text-[var(--color-muted-foreground)]')
            }
          >
            {t('automations.tabYours')}
          </button>
          <button
            type="button"
            onClick={() => setView('templates')}
            className={
              'rounded-full px-2.5 py-1 text-xs transition-colors ' +
              (view === 'templates'
                ? 'bg-[var(--color-surface)] text-[var(--color-foreground)] shadow-[var(--shadow-sm)]'
                : 'text-[var(--color-muted-foreground)]')
            }
          >
            {t('automations.tabTemplates')}
          </button>
        </div>
        <button
          type="button"
          onClick={newAutomation}
          className="inline-flex items-center gap-1.5 rounded-[var(--radius-lg)] bg-[var(--color-primary)] px-3 py-1.5 text-[12.5px] font-medium text-[var(--color-on-primary)] transition-opacity hover:opacity-90"
        >
          <PlusIcon className="h-3.5 w-3.5" />
          {t('automations.new')}
        </button>
      </header>

      <div className="min-h-0 flex-1 overflow-y-auto">
        <div className="mx-auto w-full max-w-3xl px-5">
          {view === 'list' ? (
            loading && !items.length ? (
              <div className="py-7 text-[13.5px] text-[var(--color-muted-foreground)]">{t('common.loading')}</div>
            ) : !items.length ? (
              // ── Empty state ──
              <div className="flex flex-1 flex-col items-center justify-center py-16 text-center">
                <div className="mb-1 grid h-10 w-10 place-items-center rounded-[var(--radius-lg)] border border-[var(--color-border)] bg-[var(--color-surface)] text-[var(--color-muted-foreground)]">
                  <BoltIcon className="h-5 w-5" />
                </div>
                <h2 className="mt-2.5 text-[15px] font-semibold text-[var(--color-foreground)]">{t('automations.emptyTitle')}</h2>
                <p className="mt-2.5 max-w-[380px] text-[13px] leading-relaxed text-[var(--color-muted-foreground)]">
                  {t('automations.lede')}
                </p>
                <div className="mt-3 flex gap-2">
                  <button
                    type="button"
                    onClick={newAutomation}
                    className="rounded-[var(--radius-lg)] bg-[var(--color-primary)] px-3 py-1.5 text-[12.5px] font-medium text-[var(--color-on-primary)]"
                  >
                    {t('automations.new')}
                  </button>
                  <button
                    type="button"
                    onClick={() => setView('templates')}
                    className="rounded-[var(--radius-lg)] border border-[var(--color-border)] bg-[var(--color-surface)] px-3 py-1.5 text-[12.5px] font-medium transition-colors hover:bg-[var(--color-muted)]"
                  >
                    {t('automations.browseTemplates')}
                  </button>
                </div>
              </div>
            ) : (
              <>
                <p className="py-[18px] text-[13px] leading-relaxed text-[var(--color-muted-foreground)]">
                  {t('automations.lede')}
                </p>

                {/* ── Cards ── */}
                <div className="grid grid-cols-[repeat(auto-fill,minmax(300px,1fr))] gap-3.5 pb-1.5">
                  {items.map((a) => {
                    const cs = cardState(a)
                    const stripColor =
                      cs === 'success'
                        ? 'var(--color-success)'
                        : cs === 'error'
                          ? 'var(--color-destructive)'
                          : cs === 'running'
                            ? 'var(--color-primary)'
                            : 'var(--color-border)'
                    return (
                      <div
                        key={a.id}
                        className="relative flex flex-col gap-2 overflow-hidden rounded-[var(--radius-xl)] border border-[var(--color-border)] bg-[var(--color-background)] pb-3 pt-3.5 pl-3.5 pr-3.5 transition-[box-shadow] hover:shadow-[var(--shadow-sm)]"
                        style={{ opacity: cs === 'paused' ? 0.7 : 1 }}
                      >
                        {/* status strip */}
                        <span
                          aria-hidden
                          className="absolute top-0 bottom-0 left-0 w-[3px]"
                          style={{ background: stripColor }}
                        />
                        <div className="flex items-center justify-between gap-2">
                          <div className="flex min-w-0 items-center gap-2">
                            <span className="truncate text-sm font-semibold">{a.name}</span>
                            <CadenceChip a={a} />
                          </div>
                          {cs !== 'paused' && <StatusChip state={cs} />}
                        </div>

                        <p className="line-clamp-2 min-h-[2.6em] text-[12.5px] leading-relaxed text-[var(--color-muted-foreground)]">
                          {a.prompt}
                        </p>

                        {/* meta grid */}
                        <div className="mt-1 grid grid-cols-[auto_1fr] items-center gap-x-3 gap-y-1 border-t border-[var(--color-border)] pt-2.5">
                          <span className="font-mono text-[9.5px] font-semibold uppercase tracking-[0.06em] text-[var(--color-muted-foreground)]">
                            {t('automations.meta.schedule')}
                          </span>
                          <span className="truncate text-xs text-[var(--color-foreground)]">{a.human_schedule}</span>
                          <span className="font-mono text-[9.5px] font-semibold uppercase tracking-[0.06em] text-[var(--color-muted-foreground)]">
                            {usesNextRun(a) ? t('automations.meta.nextRun') : t('automations.meta.lastRun')}
                          </span>
                          <span
                            className={
                              'truncate text-xs ' +
                              (usesNextRun(a) && a.enabled
                                ? 'font-semibold tabular-nums text-[var(--color-primary)]'
                                : 'text-[var(--color-foreground)]')
                            }
                          >
                            {nextRunLabel(a, t)}
                          </span>

                          <div className="col-span-2 mt-1 flex items-center gap-0.5">
                            <button
                              type="button"
                              title={t('common.edit')}
                              onClick={() => editAutomation(a)}
                              className="cursor-pointer rounded-[var(--radius-md)] p-1 text-[var(--color-muted-foreground)] transition-colors hover:bg-[var(--color-muted)] hover:text-[var(--color-foreground)]"
                            >
                              <PencilIcon className="h-4 w-4" />
                            </button>
                            <button
                              type="button"
                              title={t('common.delete')}
                              onClick={() => void remove(a.id)}
                              className="cursor-pointer rounded-[var(--radius-md)] p-1 text-[var(--color-muted-foreground)] transition-colors hover:bg-[var(--color-muted)] hover:text-[var(--color-destructive)]"
                            >
                              <TrashIcon className="h-4 w-4" />
                            </button>
                            <div className="flex-1" />
                            <Toggle
                              checked={a.enabled}
                              onChange={(next) => void setEnabled(a, next)}
                              title={a.enabled ? t('automations.enabled') : t('automations.disabled')}
                            />
                            <button
                              type="button"
                              title={t('automations.runNow')}
                              disabled={isRunning(a)}
                              onClick={() => void runNow(a.id)}
                              className={
                                'grid h-[26px] w-[26px] cursor-pointer place-items-center rounded-[var(--radius-md)] transition-colors disabled:cursor-not-allowed disabled:opacity-45 ' +
                                (isRunning(a)
                                  ? 'text-[var(--color-primary)]'
                                  : 'text-[var(--color-muted-foreground)] hover:bg-[var(--color-muted)] hover:text-[var(--color-foreground)]')
                              }
                            >
                              <PlayIcon className="h-4 w-4" />
                            </button>
                          </div>
                        </div>
                      </div>
                    )
                  })}
                </div>

                {/* ── Recent runs ── */}
                <div className="mb-3 mt-7 flex items-center justify-between">
                  <h2 className="text-[15px] font-semibold">{t('automations.recentRuns')}</h2>
                  {runs.length > 0 && (
                    <div className="flex items-center gap-2">
                      <select
                        value={statusFilter}
                        onChange={(e) => setStatusFilter(e.target.value as StatusFilter)}
                        title={t('automations.filterRuns')}
                        className="h-8 rounded-[var(--radius-lg)] border border-[var(--color-border)] bg-[var(--color-background)] px-2 text-[12.5px] text-[var(--color-foreground)] outline-none focus:border-[var(--color-primary)]"
                      >
                        <option value="all">{t('automations.filterAll')}</option>
                        <option value="success">{t('automations.filterSuccess')}</option>
                        <option value="failed">{t('automations.filterFailed')}</option>
                      </select>
                      <input
                        value={search}
                        onChange={(e) => setSearch(e.target.value)}
                        placeholder={t('automations.searchRuns', { n: runs.length })}
                        className="h-8 min-w-[220px] rounded-[var(--radius-lg)] border border-[var(--color-border)] bg-[var(--color-background)] px-2.5 text-[12.5px] text-[var(--color-foreground)] outline-none focus:border-[var(--color-primary)]"
                      />
                    </div>
                  )}
                </div>

                {filteredRuns.length === 0 ? (
                  <div className="py-3.5 text-[13px] text-[var(--color-muted-foreground)]">{t('automations.noRuns')}</div>
                ) : (
                  <div className="mb-6 overflow-hidden rounded-[var(--radius-xl)] border border-[var(--color-border)] bg-[var(--color-background)]">
                    {filteredRuns.map((r, idx) => {
                      const st =
                        r.terminal_status === 'success'
                          ? 'success'
                          : r.terminal_status === 'error'
                            ? 'error'
                            : 'running'
                      const barColor =
                        st === 'success'
                          ? 'var(--color-success)'
                          : st === 'error'
                            ? 'var(--color-destructive)'
                            : 'var(--color-primary)'
                      return (
                        <div
                          key={r.session_id}
                          role="button"
                          tabIndex={0}
                          onClick={() => onOpenRun?.(r)}
                          onKeyDown={(e) => {
                            if (e.key === 'Enter' || e.key === ' ') {
                              e.preventDefault()
                              onOpenRun?.(r)
                            }
                          }}
                          className={
                            'relative block w-full cursor-pointer px-4 py-3 pl-[18px] text-left transition-colors hover:bg-[var(--neutral-wash-soft)] ' +
                            (idx < filteredRuns.length - 1 ? 'border-b border-[var(--color-border)]' : '')
                          }
                        >
                          <span
                            aria-hidden
                            className="absolute top-1.5 bottom-1.5 left-0 w-[3px] rounded-full"
                            style={{ background: barColor }}
                          />
                          <div className="flex min-w-0 flex-col gap-0.5">
                            <span className="text-[13.5px] font-medium">{r.title || t('automations.runFallback')}</span>
                            <div className="inline-flex items-center gap-2 text-[11.5px] tabular-nums text-[var(--color-muted-foreground)]">
                              <span>{runLabel(r)}</span>
                              <span className="text-[var(--color-border)]">·</span>
                              <span className="rounded-[var(--radius-sm)] bg-[var(--color-muted)] px-1.5 py-px text-[10.5px] font-semibold text-[var(--color-muted-foreground)]">
                                {triggerKindLabel(r.trigger_kind, t)}
                              </span>
                              {!!r.artifact_count && (
                                <span className="inline-flex items-center gap-1 rounded-[var(--radius-sm)] bg-[var(--color-muted)] px-1.5 py-px text-[10.5px] font-semibold text-[var(--color-muted-foreground)]">
                                  <DocumentDuplicateIcon className="h-3 w-3" />
                                  {r.artifact_count}
                                  {r.artifact_unseen && <span className="h-1.5 w-1.5 rounded-full bg-[var(--color-accent-neutral)]" />}
                                </span>
                              )}
                            </div>
                            {r.error_reason && (
                              <span className="mt-0.5 text-[11.5px] leading-relaxed text-[var(--color-error-fg)]">
                                {r.error_reason}
                              </span>
                            )}
                          </div>
                        </div>
                      )
                    })}
                  </div>
                )}
              </>
            )
          ) : (
            // ── Templates ──
            <>
              <p className="py-[18px] text-[13px] leading-relaxed text-[var(--color-muted-foreground)]">
                {t('automations.templatesLede')}
              </p>
              <div className="grid grid-cols-[repeat(auto-fill,minmax(300px,1fr))] gap-3.5 pb-6">
                {templates.map((tpl) => (
                  <button
                    key={tpl.id}
                    type="button"
                    onClick={() => fromTemplate(tpl)}
                    className="flex cursor-pointer flex-col gap-2 rounded-[var(--radius-xl)] border border-[var(--color-border)] bg-[var(--color-background)] p-4 text-left transition-colors hover:border-[var(--color-primary)] hover:shadow-[var(--shadow-sm)]"
                  >
                    <div className="mb-0.5 flex items-center justify-between gap-2">
                      <span className="text-sm font-semibold">{tpl.name}</span>
                      <span className="rounded-full bg-[var(--accent-wash)] px-2 py-0.5 text-[10.5px] font-semibold text-[var(--color-primary)]">
                        {tpl.badge}
                      </span>
                    </div>
                    <p className="line-clamp-2 text-[12.5px] leading-relaxed text-[var(--color-muted-foreground)]">
                      {tpl.description}
                    </p>
                    <div className="mt-0.5 flex items-center gap-1.5 text-[11.5px] text-[var(--color-muted-foreground)]">
                      {modeLabel(tpl.suggest_mode, t)} ·{' '}
                      {tpl.trigger.type === 'manual'
                        ? t('automations.triggerManual')
                        : t('automations.triggerSchedule')}
                    </div>
                  </button>
                ))}
                {!templates.length && (
                  <div className="py-7 text-[13.5px] text-[var(--color-muted-foreground)]">{t('automations.templatesNone')}</div>
                )}
              </div>
            </>
          )}
        </div>
      </div>

      {editor && (
        <AutomationEditor
          state={editor}
          onClose={() => setEditor(null)}
          onSaved={() => {
            void fetchAll()
          }}
        />
      )}
    </div>
  )
}
