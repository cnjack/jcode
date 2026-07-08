/**
 * Tool display info — ported from web/src/composables/toolInfo.ts.
 * Used when replaying sessions (no backend display_info available). Titles are
 * plain strings (no i18n here — the product app can wrap with react-i18next if
 * needed; the component library is locale-agnostic).
 */

import type { ToolDisplayInfo } from './types'

function shortenPath(path: string): string {
  if (!path) return ''
  const parts = path.replace(/\\/g, '/').split('/')
  if (parts.length <= 2) return path
  return '…/' + parts.slice(-2).join('/')
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
      return { title: 'read', icon: 'file', category: 'context', subtitle: shortenPath(getString('file_path') || getString('path')) }
    case 'write':
      return { title: 'write', icon: 'file-edit', category: 'mutation', subtitle: shortenPath(getString('file_path') || getString('path')) }
    case 'edit':
      return { title: 'edit', icon: 'file-edit', category: 'mutation', subtitle: shortenPath(getString('file_path') || getString('path')) }
    case 'multi_edit':
      return { title: 'multi-edit', icon: 'file-edit', category: 'mutation', subtitle: shortenPath(getString('file_path') || getString('path')) }
    case 'glob':
      return { title: 'glob', icon: 'search', category: 'context', subtitle: getString('pattern') }
    case 'grep':
      return { title: 'search', icon: 'search', category: 'context', subtitle: getString('pattern') }
    case 'execute': {
      let subtitle = getString('description')
      if (!subtitle) {
        const cmd = getString('command')
        subtitle = cmd.length > 100 ? cmd.slice(0, 100) + '…' : cmd
      }
      return { title: 'shell', icon: 'terminal', category: 'execution', subtitle }
    }
    case 'background':
      return { title: 'background', icon: 'terminal', category: 'execution', subtitle: getString('description') }
    case 'todowrite':
      return { title: 'update todos', icon: 'checklist', category: 'mutation' }
    case 'todoread':
      return { title: 'read todos', icon: 'checklist', category: 'context' }
    case 'subagent': {
      const subtitle = getString('description') || getString('name')
      return { title: 'subagent', icon: 'agent', category: 'execution', subtitle }
    }
    case 'ask_user': {
      let subtitle = getString('question')
      if (subtitle.length > 60) subtitle = subtitle.slice(0, 60) + '…'
      return { title: 'ask user', icon: 'question', category: 'context', subtitle }
    }
    default:
      return { title: name, icon: 'tool', category: '' }
  }
}
