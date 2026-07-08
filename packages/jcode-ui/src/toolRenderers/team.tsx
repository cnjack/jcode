/**
 * TeamRenderers — renders team_* tool calls.
 * Three renderers: team_list (member table), team_send_message (message card),
 * team_create (team summary). Output formats parsed from the Vue ToolCallCard.
 */

import { memo, useMemo } from 'react'
import type { ToolRendererProps } from 'jcode-ui-core/adapters'

interface TeamMember {
  name: string
  status: string
  type: string
  progress: string
}

export const TeamListRenderer = memo(function TeamListRenderer({ output }: ToolRendererProps) {
  const { teamName, members } = useMemo(() => parseTeamList(output), [output])
  return (
    <div className="jcode-team-list my-1 rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--code-bg)] px-3 py-2">
      {teamName && (
        <div className="mb-2 font-medium text-[var(--color-foreground)]">
          Team: {teamName} <span className="text-[var(--color-muted-foreground)]">({members.length})</span>
        </div>
      )}
      <table className="w-full text-[0.78rem]">
        <thead>
          <tr className="text-left text-[0.7rem] text-[var(--color-muted-foreground)]">
            <th className="py-1 pr-2">member</th>
            <th className="py-1 pr-2">status</th>
            <th className="py-1 pr-2">type</th>
            <th className="py-1">progress</th>
          </tr>
        </thead>
        <tbody>
          {members.map((m, i) => (
            <tr key={i} className="border-t border-[var(--color-border)]">
              <td className="py-1 pr-2 text-[var(--color-primary)]">@{m.name}</td>
              <td className="py-1 pr-2 text-[var(--color-foreground)]">{m.status}</td>
              <td className="py-1 pr-2 text-[var(--color-muted-foreground)]">{m.type}</td>
              <td className="py-1 text-[var(--color-muted-foreground)]">{m.progress}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
})

export const TeamMessageRenderer = memo(function TeamMessageRenderer({ args, output }: ToolRendererProps) {
  const { to, message } = useMemo(() => {
    let t = ''
    let m = ''
    try {
      const parsed = JSON.parse(args)
      t = parsed.to ?? parsed.recipient ?? ''
      m = parsed.message ?? parsed.content ?? ''
    } catch {
      // ignore
    }
    if (!m && output) m = output
    return { to: t, message: m }
  }, [args, output])
  return (
    <div className="jcode-team-message my-1 rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--code-bg)] px-3 py-2">
      {to && <div className="text-[0.78rem] text-[var(--color-muted-foreground)]">→ @{to}</div>}
      <div className="mt-1 text-[0.82rem] text-[var(--color-foreground)]">{message}</div>
    </div>
  )
})

export const TeamCreateRenderer = memo(function TeamCreateRenderer({ output }: ToolRendererProps) {
  const teamName = useMemo(() => {
    const m = (output ?? '').match(/Team:\s*(.+?)(?:\s+\(|$)/)
    return m ? m[1] ?? '' : ''
  }, [output])
  return (
    <div className="jcode-team-create my-1 rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--code-bg)] px-3 py-2">
      <span className="text-[0.82rem] text-[var(--color-foreground)]">Created team </span>
      <span className="font-medium text-[var(--color-primary)]">{teamName}</span>
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
