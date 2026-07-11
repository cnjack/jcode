/**
 * CanvasPanel — a freely-positioned overlay panel over the canvas viewport.
 *
 * Thin wrapper over React Flow's `<Panel>` with a token-driven card surface
 * (surface / radius-lg / shadow-md). Use it for legends, titles, filters or
 * any floating chrome. Must render inside a `<WorkflowCanvas>` / `<ReactFlow>`.
 */

import { memo } from 'react'
import type { ReactNode } from 'react'
import { Panel } from '@xyflow/react'
import type { PanelPosition } from '@xyflow/react'

export interface CanvasPanelProps {
  /** Corner to dock the panel (default 'top-right'). */
  position?: PanelPosition
  className?: string
  children?: ReactNode
}

export const CanvasPanel = memo(function CanvasPanel({
  position = 'top-right',
  className,
  children,
}: CanvasPanelProps) {
  return (
    <Panel
      position={position}
      data-jcode-ui=""
      className={`jcode-wf-panel ${className ?? ''}`.trimEnd()}
    >
      {children}
    </Panel>
  )
})
