/**
 * toolIcons — map a tool's displayInfo.kind (or name) to a Heroicon.
 *
 * Used by compact activity rows (e.g. ActivityGroupCard's running header) to
 * show a 12px icon that matches the tool category. Mirrors the kind taxonomy
 * documented in jcode-ui-core's ToolDisplayInfo:
 *   read | search | list | shell | edit | agent | other
 * plus name-based fallbacks for tools that don't set `kind` (execute / bash /
 * grep / glob / write / multi_edit / webfetch / todo / browser…).
 */

import {
  CheckCircleIcon,
  CommandLineIcon,
  ComputerDesktopIcon,
  DocumentPlusIcon,
  DocumentTextIcon,
  GlobeAltIcon,
  ListBulletIcon,
  MagnifyingGlassIcon,
  PencilSquareIcon,
  UserCircleIcon,
  WrenchScrewdriverIcon,
} from '@heroicons/react/24/outline'
import type { ComponentType, SVGProps } from 'react'

export type ToolIconProps = SVGProps<SVGSVGElement> & {
  /** Presentation kind from ToolDisplayInfo (read | search | list | shell | edit | agent | other). */
  kind?: string
  /** Raw tool name; used as fallback when kind is absent. */
  name?: string
}

type Icon = ComponentType<SVGProps<SVGSVGElement>>

const BY_KIND: Record<string, Icon> = {
  read: DocumentTextIcon,
  search: MagnifyingGlassIcon,
  list: ListBulletIcon,
  shell: CommandLineIcon,
  edit: PencilSquareIcon,
  write: DocumentPlusIcon,
  agent: UserCircleIcon,
  web: GlobeAltIcon,
  browser: ComputerDesktopIcon,
  todo: CheckCircleIcon,
}

const BY_NAME: Record<string, Icon> = {
  read: DocumentTextIcon,
  grep: MagnifyingGlassIcon,
  glob: MagnifyingGlassIcon,
  list_dir: ListBulletIcon,
  execute: CommandLineIcon,
  bash: CommandLineIcon,
  edit: PencilSquareIcon,
  multi_edit: PencilSquareIcon,
  write: DocumentPlusIcon,
  subagent: UserCircleIcon,
  team: UserCircleIcon,
  webfetch: GlobeAltIcon,
  websearch: GlobeAltIcon,
  todoread: CheckCircleIcon,
  todowrite: CheckCircleIcon,
  todo: CheckCircleIcon,
  browser: ComputerDesktopIcon,
}

const FALLBACK_ICON = WrenchScrewdriverIcon

/** Resolve a Heroicon for a tool by kind (preferred) then name. */
export function ToolIcon({ kind, name, ...rest }: ToolIconProps) {
  const Icon = (kind && BY_KIND[kind]) || (name && BY_NAME[name]) || FALLBACK_ICON
  return <Icon {...rest} />
}
