// Automation types — mirror internal/automation + internal/web/automation_api.go.

export type AutomationCadence = 'hourly' | 'daily' | 'weekly'

export interface AutomationTrigger {
  type: 'schedule' | 'manual'
  cadence?: AutomationCadence
  hour?: number
  minute?: number
  weekday?: number
}

export interface Automation {
  id: string
  name: string
  prompt: string
  trigger: AutomationTrigger
  project_path: string
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
