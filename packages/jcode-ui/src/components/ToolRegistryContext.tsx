/**
 * ToolRendererRegistry context — the host provides a registry (built with
 * createDefaultToolRegistry or a custom one) once at the app root, and every
 * ToolCallCard reads from it. This avoids prop-drilling the registry through
 * every component.
 */

import { createContext, useContext } from 'react'
import type { ReactNode } from 'react'
import { createToolRendererRegistry } from 'jcode-ui-core/adapters'
import type { ToolRendererRegistry, ToolRenderer } from 'jcode-ui-core/adapters'
import { TerminalRenderer } from '../toolRenderers/terminal.js'
import { FileViewerRenderer } from '../toolRenderers/fileViewer.js'
import { DiffRenderer } from '../toolRenderers/diff.js'
import { SearchRenderer } from '../toolRenderers/search.js'
import { TodoRenderer } from '../toolRenderers/todo.js'
import { SkillRenderer } from '../toolRenderers/skill.js'
import {
  TeamListRenderer,
  TeamMessageRenderer,
  TeamCreateRenderer,
  TeamSpawnRenderer,
} from '../toolRenderers/team.js'
import { BrowserShotRenderer } from '../toolRenderers/browserShot.js'
import { FileTreeRenderer } from '../toolRenderers/fileTree.js'
import { GenericRenderer } from '../toolRenderers/generic.js'

const Ctx = createContext<ToolRendererRegistry | null>(null)

/** Build the jcode default registry (matches Vue ToolCallCard renderType map). */
export function createDefaultToolRegistry(): ToolRendererRegistry {
  const r = createToolRendererRegistry()
  const map: Record<string, ToolRenderer> = {
    execute: TerminalRenderer,
    read: FileViewerRenderer,
    write: FileViewerRenderer,
    edit: DiffRenderer,
    multi_edit: DiffRenderer,
    grep: SearchRenderer,
    todowrite: TodoRenderer,
    todoread: TodoRenderer,
    load_skill: SkillRenderer,
    team_list: TeamListRenderer,
    team_send_message: TeamMessageRenderer,
    team_create: TeamCreateRenderer,
    team_spawn: TeamSpawnRenderer,
    browser_screenshot: BrowserShotRenderer,
    list_dir: FileTreeRenderer,
    glob: FileTreeRenderer,
  }
  r.registerAll(map)
  r.setFallback(GenericRenderer)
  return r
}

const defaultReg = createDefaultToolRegistry()

export interface ToolRegistryProviderProps {
  /** Defaults to createDefaultToolRegistry() if omitted. */
  registry?: ToolRendererRegistry
  children: ReactNode
}

export function ToolRegistryProvider({ registry, children }: ToolRegistryProviderProps) {
  const reg = registry ?? defaultReg
  return <Ctx.Provider value={reg}>{children}</Ctx.Provider>
}

export function useToolRegistry(): ToolRendererRegistry {
  const r = useContext(Ctx)
  return r ?? defaultReg
}
