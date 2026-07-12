/**
 * exportThreadMarkdown — serialize a conversation timeline to portable
 * GitHub-flavored markdown.
 *
 * Pure and deterministic: no clock access (pass `now` for the header stamp)
 * and no DOM. Tool calls fold into <details> so long transcripts stay
 * scannable; fences inside tool output are escaped by widening the outer
 * fence, never by mangling the content.
 */

import type { ThreadItem, ToolCall, Message, Approval, ExploringGroup } from '../types/index.js'

export interface ExportMarkdownOptions {
  /** Document H1. Default: "Conversation". */
  title?: string
  /** Timestamp for the header line; omitted → no stamp (deterministic). */
  now?: Date
  /** Truncate a single tool output beyond this many chars. Default 4000. */
  maxToolOutput?: number
  /** Role display labels. */
  labels?: { user?: string; assistant?: string; system?: string }
}

/** Pick a fence longer than any run of backticks inside `body`. */
function fenceFor(body: string): string {
  const longest = body.match(/`{3,}/g)?.reduce((m, s) => Math.max(m, s.length), 0) ?? 0
  return '`'.repeat(Math.max(3, longest + 1))
}

function codeBlock(body: string, lang = ''): string {
  const fence = fenceFor(body)
  return `${fence}${lang}\n${body}\n${fence}`
}

function truncate(s: string, max: number): string {
  if (s.length <= max) return s
  return `${s.slice(0, max)}\n… (${s.length - max} chars truncated)`
}

function prettyJSON(raw: string): string {
  try {
    return JSON.stringify(JSON.parse(raw), null, 2)
  } catch {
    return raw
  }
}

function renderMessage(m: Message, labels: Required<NonNullable<ExportMarkdownOptions['labels']>>): string {
  const label = m.role === 'user' ? labels.user : m.role === 'assistant' ? labels.assistant : labels.system
  const parts = [`### ${label}`]
  if (m.reasoning) {
    parts.push(`<details><summary>Reasoning</summary>\n\n${m.reasoning}\n\n</details>`)
  }
  parts.push(m.content.trim() || '_(empty)_')
  if (m.sources?.length) {
    parts.push(m.sources.map((s) => `- [${s.title}](${s.url ?? ''})`).join('\n'))
  }
  return parts.join('\n\n')
}

function renderTool(t: ToolCall, maxOutput: number, depth = 0): string {
  const title = t.displayInfo?.title ?? t.name
  const subtitle = t.displayInfo?.subtitle ? ` — ${t.displayInfo.subtitle}` : ''
  const status = t.status === 'done' ? '' : ` (${t.status})`
  const body: string[] = []
  const args = prettyJSON(t.args)
  if (args && args !== '{}') body.push(codeBlock(args, 'json'))
  const output = t.displayOutput ?? t.output
  if (output) body.push(codeBlock(truncate(output, maxOutput)))
  if (t.error) body.push(codeBlock(truncate(t.error, maxOutput)))
  for (const child of t.children ?? []) body.push(renderTool(child, maxOutput, depth + 1))
  return [
    `<details><summary>🔧 ${title}${subtitle}${status}</summary>`,
    '',
    body.join('\n\n') || '_(no output)_',
    '',
    '</details>',
  ].join('\n')
}

function renderApproval(a: Approval): string {
  const chosen = a.resolvedOptionId
    ? a.options?.find((o) => o.id === a.resolvedOptionId)?.label
    : undefined
  const outcome = a.resolved ? (chosen ?? (a.approved ? 'Allowed' : 'Denied')) : 'Pending'
  return `> 🛡️ Approval — \`${a.tool_name}\`: **${outcome}**${a.is_external ? ' _(external target)_' : ''}`
}

function renderExploring(g: ExploringGroup, maxOutput: number): string {
  const steps = g.tools
    .map((t) => `- ${t.displayInfo?.title ?? t.name}${t.displayInfo?.subtitle ? ` ${t.displayInfo.subtitle}` : ''}`)
    .join('\n')
  void maxOutput
  return `<details><summary>🔍 Explored ${g.tools.length} steps</summary>\n\n${steps}\n\n</details>`
}

export function exportThreadMarkdown(items: ThreadItem[], opts: ExportMarkdownOptions = {}): string {
  const labels = {
    user: opts.labels?.user ?? 'You',
    assistant: opts.labels?.assistant ?? 'Assistant',
    system: opts.labels?.system ?? 'System',
  }
  const maxOutput = opts.maxToolOutput ?? 4000
  const out: string[] = [`# ${opts.title ?? 'Conversation'}`]
  if (opts.now) out.push(`_Exported ${opts.now.toISOString()}_`)
  for (const item of items) {
    switch (item.kind) {
      case 'message':
        out.push(renderMessage(item.data, labels))
        break
      case 'tool':
        out.push(renderTool(item.data, maxOutput))
        break
      case 'approval':
        out.push(renderApproval(item.data))
        break
      case 'exploring':
        out.push(renderExploring(item.data, maxOutput))
        break
      case 'batch':
        // Batches are UI-only grouping — export each member as a plain tool.
        for (const t of item.data.tools) out.push(renderTool(t, maxOutput))
        break
    }
  }
  return out.join('\n\n') + '\n'
}
