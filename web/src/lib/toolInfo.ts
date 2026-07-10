/**
 * Tool display info — ported from web/src/composables/toolInfo.ts.
 * Used when replaying sessions (no backend display_info available).
 */

import type { ToolDisplayInfo } from './types'

function shortenPath(path: string): string {
  if (!path) return ''
  const parts = path.replace(/\\/g, '/').split('/')
  if (parts.length <= 2) return path
  return '…/' + parts.slice(-2).join('/')
}

/** Rough client-side classify for execute when backend kind is absent. */
function classifyExecute(cmd: string): { kind: string; collapsible: boolean } {
  const base = (cmd.trim().split(/\s+/)[0] || '').split('/').pop() || ''
  if (['cat', 'head', 'tail', 'less', 'jq', 'wc'].includes(base)) return { kind: 'read', collapsible: true }
  if (['grep', 'rg', 'find', 'ag'].includes(base)) return { kind: 'search', collapsible: true }
  if (['ls', 'tree', 'du', 'df', 'pwd', 'echo', 'date', 'whoami', 'which'].includes(base)) {
    return { kind: 'list', collapsible: true }
  }
  if (base === 'git') {
    const sub = cmd.trim().split(/\s+/)[1] || ''
    if (['status', 'log', 'diff', 'show', 'branch'].includes(sub)) return { kind: 'search', collapsible: true }
  }
  return { kind: 'shell', collapsible: false }
}

export function extractToolDisplayInfo(name: string, argsJSON: string): ToolDisplayInfo {
  let args: Record<string, unknown> = {}
  try {
    args = JSON.parse(argsJSON) || {}
  } catch {
    // ignore
  }
  const getString = (key: string): string => {
    const v = args[key]
    return typeof v === 'string' ? v : ''
  }

  switch (name) {
    case 'read':
      return {
        title: 'read',
        icon: 'file',
        category: 'context',
        kind: 'read',
        collapsible: true,
        subtitle: shortenPath(getString('file_path') || getString('path')),
      }
    case 'write':
      return {
        title: 'write',
        icon: 'file-edit',
        category: 'mutation',
        kind: 'edit',
        collapsible: false,
        subtitle: shortenPath(getString('file_path') || getString('path')),
      }
    case 'edit':
      return {
        title: 'edit',
        icon: 'file-edit',
        category: 'mutation',
        kind: 'edit',
        collapsible: false,
        subtitle: shortenPath(getString('file_path') || getString('path')),
      }
    case 'multi_edit':
      return {
        title: 'multi-edit',
        icon: 'file-edit',
        category: 'mutation',
        kind: 'edit',
        collapsible: false,
        subtitle: shortenPath(getString('file_path') || getString('path')),
      }
    case 'glob':
      return {
        title: 'glob',
        icon: 'search',
        category: 'context',
        kind: 'search',
        collapsible: true,
        subtitle: getString('pattern'),
      }
    case 'grep':
      return {
        title: 'search',
        icon: 'search',
        category: 'context',
        kind: 'search',
        collapsible: true,
        subtitle: getString('pattern'),
      }
    case 'execute': {
      let subtitle = getString('description')
      const cmd = getString('command')
      if (!subtitle) {
        subtitle = cmd.length > 100 ? cmd.slice(0, 100) + '…' : cmd
      }
      const { kind, collapsible } = classifyExecute(cmd)
      return { title: 'shell', icon: 'terminal', category: 'execution', kind, collapsible, subtitle }
    }
    case 'background':
      return {
        title: 'background',
        icon: 'terminal',
        category: 'execution',
        kind: 'shell',
        collapsible: false,
        subtitle: getString('description'),
      }
    case 'todowrite':
      return { title: 'update todos', icon: 'checklist', category: 'mutation', kind: 'edit', collapsible: false }
    case 'todoread':
      return { title: 'read todos', icon: 'checklist', category: 'context', kind: 'read', collapsible: true }
    case 'subagent': {
      const subtitle = getString('description') || getString('name')
      return { title: 'subagent', icon: 'agent', category: 'execution', kind: 'agent', collapsible: false, subtitle }
    }
    case 'ask_user': {
      let subtitle = getString('question')
      if (subtitle.length > 60) subtitle = subtitle.slice(0, 60) + '…'
      return { title: 'ask user', icon: 'question', category: 'context', kind: 'other', collapsible: false, subtitle }
    }
    default:
      return { title: name, icon: 'tool', category: '', kind: 'other', collapsible: false }
  }
}
