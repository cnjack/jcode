// Tool display information utilities for client-side rendering.
// Used when replaying sessions (where the backend display_info is not available).

import type { ToolDisplayInfo } from '@/types/api'
import { i18n } from '@/i18n'

/** Extract the last 2 path components for compact display. */
function shortenPath(path: string): string {
  if (!path) return ''
  const parts = path.replace(/\\/g, '/').split('/')
  if (parts.length <= 2) return path
  return '…/' + parts.slice(-2).join('/')
}

/**
 * Extract display metadata from a tool name and its JSON args.
 * Mirrors the backend extractToolDisplayInfo function for session replay.
 */
export function extractToolDisplayInfo(name: string, argsJSON: string): ToolDisplayInfo {
  let args: Record<string, unknown> = {}
  try {
    args = JSON.parse(argsJSON) || {}
  } catch {
    // ignore parse errors
  }

  const getString = (key: string): string => {
    const v = args[key]
    return typeof v === 'string' ? v : ''
  }

  const t = i18n.global.t.bind(i18n.global)
  switch (name) {
    case 'read':
      return { title: t('tool.read'), icon: 'file', category: 'context', subtitle: shortenPath(getString('file_path')) }
    case 'write':
      return { title: t('tool.write'), icon: 'file-edit', category: 'mutation', subtitle: shortenPath(getString('file_path')) }
    case 'edit':
      return { title: t('tool.edit'), icon: 'file-edit', category: 'mutation', subtitle: shortenPath(getString('file_path')) }
    case 'multi_edit':
      return { title: t('tool.multiEdit'), icon: 'file-edit', category: 'mutation', subtitle: shortenPath(getString('file_path')) }
    case 'glob':
      return { title: t('tool.glob'), icon: 'search', category: 'context', subtitle: getString('pattern') }
    case 'grep':
      return { title: t('tool.search'), icon: 'search', category: 'context', subtitle: getString('pattern') }
    case 'execute': {
      let subtitle = getString('description')
      if (!subtitle) {
        const cmd = getString('command')
        subtitle = cmd.length > 100 ? cmd.slice(0, 100) + '…' : cmd
      }
      return { title: t('tool.shell'), icon: 'terminal', category: 'execution', subtitle }
    }
    case 'background':
      return { title: t('tool.background'), icon: 'terminal', category: 'execution', subtitle: getString('description') }
    case 'todowrite':
      return { title: t('tool.updateTodos'), icon: 'checklist', category: 'mutation' }
    case 'todoread':
      return { title: t('tool.readTodos'), icon: 'checklist', category: 'context' }
    case 'subagent': {
      const subtitle = getString('description') || getString('name')
      return { title: t('tool.subagent'), icon: 'agent', category: 'execution', subtitle }
    }
    case 'ask_user': {
      let subtitle = getString('question')
      if (subtitle.length > 60) subtitle = subtitle.slice(0, 60) + '…'
      return { title: t('tool.askUser'), icon: 'question', category: 'context', subtitle }
    }
    default:
      return { title: name, icon: 'tool', category: '' }
  }
}
