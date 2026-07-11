/**
 * CanvasControls — token-styled zoom / fit controls.
 *
 * A drop-in replacement for React Flow's built-in `<Controls>`, rendered as a
 * floating `<Panel>` so it positions itself over the viewport. Uses the shared
 * `useReactFlow()` imperative API, so it must live inside a `<WorkflowCanvas>`
 * (or any `<ReactFlow>` / `<ReactFlowProvider>`). Styling: `./canvas.css`.
 */

import { memo } from 'react'
import { Panel, useReactFlow } from '@xyflow/react'
import type { PanelPosition } from '@xyflow/react'
import { PlusIcon, MinusIcon, ArrowsPointingOutIcon } from '@heroicons/react/24/outline'

export interface CanvasControlsProps {
  /** Corner to dock the controls (default 'bottom-left'). */
  position?: PanelPosition
  className?: string
}

export const CanvasControls = memo(function CanvasControls({
  position = 'bottom-left',
  className,
}: CanvasControlsProps) {
  const { zoomIn, zoomOut, fitView } = useReactFlow()
  return (
    <Panel
      position={position}
      data-jcode-ui=""
      className={`jcode-wf-controls ${className ?? ''}`.trimEnd()}
    >
      <button
        type="button"
        className="jcode-wf-controls__btn"
        onClick={() => zoomIn()}
        aria-label="Zoom in"
      >
        <PlusIcon className="jcode-wf-controls__icon" />
      </button>
      <button
        type="button"
        className="jcode-wf-controls__btn"
        onClick={() => zoomOut()}
        aria-label="Zoom out"
      >
        <MinusIcon className="jcode-wf-controls__icon" />
      </button>
      <button
        type="button"
        className="jcode-wf-controls__btn"
        onClick={() => fitView()}
        aria-label="Fit view"
      >
        <ArrowsPointingOutIcon className="jcode-wf-controls__icon" />
      </button>
    </Panel>
  )
})
