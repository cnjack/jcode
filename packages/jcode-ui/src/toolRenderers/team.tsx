/**
 * Team renderers — team_list / team_send_message / team_create / team_spawn.
 * Layouts match Vue ToolCallCard.vue (dot rows, not tables). No outer borders.
 */

import { memo, useMemo } from 'react'
import type { ToolRendererProps } from 'jcode-ui-core/adapters'
import { truncate } from './terminal.js'

interface TeamMember {
  name: string
  status: string
  type: string
  progress: string
}

function memberStatusColor(status: string): string {
  if (status === 'running' || status === 'busy') return 'var(--color-accent-neutral)'
  if (status === 'done' || status === 'finished') return 'var(--color-success-fg)'
  if (status === 'error') return 'var(--color-destructive, var(--color-error-fg))'
  return 'var(--color-muted-foreground)'
}

export const TeamListRenderer = memo(function TeamListRenderer({
  output,
  error,
  status,
}: ToolRendererProps) {
  const { teamName, members } = useMemo(() => parseTeamList(output), [output])
  return (
    <div className="jcode-team-list max-h-64 overflow-y-auto px-3 py-2" style={{ background: 'var(--color-surface)' }}>
      {teamName && (
        <div
          className="mb-2 flex items-center gap-2 pb-1.5"
          style={{ borderBottom: '1px solid var(--color-border)' }}
        >
          <span
            className="text-[10px] font-semibold uppercase tracking-wider"
            style={{ color: 'var(--color-muted-foreground)' }}
          >
            Team
          </span>
          <span className="font-mono text-xs font-semibold" style={{ color: 'var(--color-foreground)' }}>
            {teamName}
          </span>
          <span
            className="ml-auto text-[10px] tabular-nums"
            style={{ color: 'var(--color-muted-foreground)' }}
          >
            {members.length} member{members.length === 1 ? '' : 's'}
          </span>
        </div>
      )}
      {members.length > 0 ? (
        <div className="space-y-0.5">
          {members.map((m, i) => (
            <div key={`${m.name}-${i}`} className="flex items-center gap-2 py-0.5">
              <span
                className="w-3 shrink-0 text-center text-[11px]"
                style={{ color: memberStatusColor(m.status) }}
              >
                ●
              </span>
              <span className="flex-1 font-mono text-xs" style={{ color: 'var(--color-foreground)' }}>
                @{m.name}
              </span>
              <span
                className="rounded px-1.5 py-0.5 font-mono text-[10px] tabular-nums"
                style={{ background: 'var(--color-muted)', color: 'var(--color-muted-foreground)' }}
              >
                {m.status}
              </span>
              {m.type && (
                <span className="text-[10px]" style={{ color: 'var(--color-muted-foreground)' }}>
                  {m.type}
                </span>
              )}
            </div>
          ))}
        </div>
      ) : status === 'running' ? (
        <div className="animate-pulse py-1 text-xs" style={{ color: 'var(--color-muted-foreground)' }}>
          Loading…
        </div>
      ) : (
        <div className="py-1 text-xs italic" style={{ color: 'var(--color-muted-foreground)' }}>
          No teammates
        </div>
      )}
      {error && (
        <div
          className="mt-1.5 font-mono text-xs"
          style={{ color: 'var(--color-destructive, var(--color-error-fg))' }}
        >
          {error}
        </div>
      )}
    </div>
  )
})

export const TeamCreateRenderer = memo(function TeamCreateRenderer({
  args,
  output,
  error,
  status,
}: ToolRendererProps) {
  const data = useMemo(() => {
    try {
      const parsed = JSON.parse(args)
      const leadMatch = (output ?? '').match(/Lead agent: (\S+)/)
      return {
        teamName: (parsed.team_name || '') as string,
        description: (parsed.description || '') as string,
        lead: leadMatch ? (leadMatch[1] ?? '') : '',
      }
    } catch {
      return { teamName: '', description: '', lead: '' }
    }
  }, [args, output])

  return (
    <div className="jcode-team-create px-3 py-2.5" style={{ background: 'var(--color-surface)' }}>
      <div className="mb-1 flex items-center gap-2">
        <span className="font-mono text-xs font-semibold" style={{ color: 'var(--color-foreground)' }}>
          {data.teamName}
        </span>
        {status === 'done' && !error && (
          <span className="ml-auto text-[10px] font-semibold" style={{ color: 'var(--color-accent-neutral)' }}>
            created
          </span>
        )}
        {status === 'running' && (
          <span className="ml-auto animate-pulse text-[10px]" style={{ color: 'var(--color-muted-foreground)' }}>
            creating
          </span>
        )}
      </div>
      {data.description && (
        <div className="text-[11px] leading-snug" style={{ color: 'var(--color-muted-foreground)' }}>
          {data.description}
        </div>
      )}
      {data.lead && (
        <div className="mt-1.5 font-mono text-[10px]" style={{ color: 'var(--color-muted-foreground)' }}>
          Lead {data.lead}
        </div>
      )}
      {error && (
        <div
          className="mt-1 font-mono text-xs"
          style={{ color: 'var(--color-destructive, var(--color-error-fg))' }}
        >
          {error}
        </div>
      )}
    </div>
  )
})

export const TeamSpawnRenderer = memo(function TeamSpawnRenderer({
  args,
  output,
  error,
  status,
}: ToolRendererProps) {
  const data = useMemo(() => {
    try {
      const parsed = JSON.parse(args)
      const idMatch = (output ?? '').match(/\(ID: ([^)]+)\)/)
      return {
        name: (parsed.name || '') as string,
        prompt: (parsed.prompt || '') as string,
        agentType: (parsed.agent_type || '') as string,
        id: idMatch ? (idMatch[1] ?? '') : '',
      }
    } catch {
      return { name: '', prompt: '', agentType: '', id: '' }
    }
  }, [args, output])

  return (
    <div className="jcode-team-spawn px-3 py-2.5" style={{ background: 'var(--color-surface)' }}>
      <div className="mb-1 flex items-center gap-2">
        <span className="font-mono text-xs font-semibold" style={{ color: 'var(--color-foreground)' }}>
          @{data.name}
        </span>
        {data.agentType && (
          <span
            className="rounded px-1.5 py-0.5 text-[10px]"
            style={{ background: 'var(--color-muted)', color: 'var(--color-muted-foreground)' }}
          >
            {data.agentType}
          </span>
        )}
        {status === 'done' && !error && (
          <span className="ml-auto text-[10px] font-semibold" style={{ color: 'var(--color-accent-neutral)' }}>
            running
          </span>
        )}
        {status === 'running' && (
          <span className="ml-auto animate-pulse text-[10px]" style={{ color: 'var(--color-muted-foreground)' }}>
            spawning
          </span>
        )}
      </div>
      {data.prompt && (
        <div
          className="text-[11px] leading-snug"
          style={{
            color: 'var(--color-muted-foreground)',
            fontStyle: 'italic',
            whiteSpace: 'pre-wrap',
          }}
        >
          {truncate(data.prompt, 150)}
        </div>
      )}
      {data.id && (
        <div className="mt-1.5 font-mono text-[10px]" style={{ color: 'var(--color-muted-foreground)' }}>
          ID {data.id}
        </div>
      )}
      {error && (
        <div
          className="mt-1 font-mono text-xs"
          style={{ color: 'var(--color-destructive, var(--color-error-fg))' }}
        >
          {error}
        </div>
      )}
    </div>
  )
})

export const TeamMessageRenderer = memo(function TeamMessageRenderer({
  args,
  error,
  status,
}: ToolRendererProps) {
  const data = useMemo(() => {
    try {
      const parsed = JSON.parse(args)
      return {
        to: (parsed.to || '') as string,
        message: (parsed.message || '') as string,
        summary: (parsed.summary || '') as string,
      }
    } catch {
      return { to: '', message: '', summary: '' }
    }
  }, [args])

  return (
    <div className="jcode-team-message px-3 py-2.5" style={{ background: 'var(--color-surface)' }}>
      <div className="mb-1.5 flex items-center gap-2">
        <span className="text-[10px]" style={{ color: 'var(--color-muted-foreground)' }}>
          →
        </span>
        <span className="font-mono text-xs font-semibold" style={{ color: 'var(--color-foreground)' }}>
          {data.to === '*' ? 'all' : `@${data.to}`}
        </span>
        {status === 'done' && !error && (
          <span className="ml-auto text-[10px] font-semibold" style={{ color: 'var(--color-accent-neutral)' }}>
            sent
          </span>
        )}
        {status === 'running' && (
          <span className="ml-auto animate-pulse text-[10px]" style={{ color: 'var(--color-muted-foreground)' }}>
            sending
          </span>
        )}
      </div>
      {data.summary && (
        <div className="mb-1 text-[11px] font-medium leading-snug" style={{ color: 'var(--color-foreground)' }}>
          {data.summary}
        </div>
      )}
      {data.message && (
        <div
          className="text-[11px] leading-snug"
          style={{
            color: 'var(--color-muted-foreground)',
            fontStyle: 'italic',
            whiteSpace: 'pre-wrap',
          }}
        >
          {truncate(data.message, 200)}
        </div>
      )}
      {error && (
        <div
          className="mt-1.5 font-mono text-xs"
          style={{ color: 'var(--color-destructive, var(--color-error-fg))' }}
        >
          {error}
        </div>
      )}
    </div>
  )
})

function parseTeamList(output?: string): { teamName: string; members: TeamMember[] } {
  const out: TeamMember[] = []
  let teamName = ''
  const teamMatch = (output ?? '').match(/^Team: (.+?) \((\d+)/)
  if (teamMatch) teamName = teamMatch[1] ?? ''
  for (const line of (output ?? '').split('\n')) {
    const m = line.match(/@(\S+)\s+status=(\S+)\s+type=(\S*)(.*)/)
    if (m) {
      out.push({
        name: m[1] ?? '',
        status: m[2] ?? '',
        type: m[3] ?? '',
        progress: (m[4] ?? '').trim(),
      })
    }
  }
  return { teamName, members: out }
}
