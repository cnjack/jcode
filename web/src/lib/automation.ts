// Automation types — mirror internal/automation + internal/web/automation_api.go.

import type { TFunction } from 'i18next'
import type { WorkspaceKind } from './types'

export type AutomationCadence = 'hourly' | 'daily' | 'weekly' | 'cron'

export interface AutomationTrigger {
  type: 'schedule' | 'manual' | 'once'
  cadence?: AutomationCadence
  hour?: number
  minute?: number
  weekday?: number
  /** 5-field cron expression (type=schedule && cadence=cron). */
  expr?: string
  /** RFC3339 pinned time (type=once). */
  at?: string
}

export interface Automation {
  id: string
  name: string
  prompt: string
  trigger: AutomationTrigger
  project_path: string
  context_policy?: 'isolated' | 'conversation'
  owner_session_id?: string
  mode: string
  provider?: string
  model?: string
  run_in_cloud: boolean
  enabled: boolean
  source: string
  created_at: string
  updated_at: string
}

export interface AutomationRunState {
  last_run_at?: string
  last_status?: string
  last_error?: string
  last_session_id?: string
  next_run_at?: string
}

export interface AutomationItem extends Automation {
  human_schedule: string
  badge: string
  state: AutomationRunState
  workspace_kind: WorkspaceKind
}

export interface AutomationRun {
  session_id: string
  automation_id: string
  title: string
  project: string
  trigger_kind: string
  start_time: string
  end_time?: string
  terminal_status?: string
  status?: string
  error_reason?: string
  artifact_count?: number
  artifact_unseen?: boolean
}

export interface AutomationTemplate {
  id: string
  name: string
  description: string
  badge: string
  prompt: string
  trigger: AutomationTrigger
  suggest_mode: string
}

export type AutomationCreate = Partial<Omit<Automation, 'id' | 'created_at' | 'updated_at'>> & {
  run_now?: boolean
}

/**
 * Localize a run's trigger kind. A run's kind is 'manual' or 'scheduled'
 * (internal/automation KindManual/KindScheduled); 'schedule' is accepted too for
 * safety. Falls back to the raw value for anything unknown.
 */
export function triggerKindLabel(kind: string, t: TFunction): string {
  if (kind === 'manual') return t('automations.triggerManual')
  if (kind === 'scheduled' || kind === 'schedule') return t('automations.triggerSchedule')
  return kind
}

/**
 * Format a Date as an RFC3339 timestamp carrying the host's local offset.
 * `toISOString()` would pin UTC and make server-side display render the wrong
 * wall-clock time; automations are scheduled in local time.
 */
export function toLocalRFC3339(d: Date): string {
  const pad = (n: number) => String(n).padStart(2, '0')
  const offsetMin = -d.getTimezoneOffset()
  const sign = offsetMin >= 0 ? '+' : '-'
  const abs = Math.abs(offsetMin)
  return (
    `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}` +
    `T${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}` +
    `${sign}${pad(Math.floor(abs / 60))}:${pad(abs % 60)}`
  )
}

/** Convert an RFC3339 timestamp to a datetime-local input value (minute precision, local wall time). Empty on unparseable input. */
export function toDatetimeLocal(iso: string | undefined): string {
  if (!iso) return ''
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''
  const pad = (n: number) => String(n).padStart(2, '0')
  return (
    `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}` +
    `T${pad(d.getHours())}:${pad(d.getMinutes())}`
  )
}
