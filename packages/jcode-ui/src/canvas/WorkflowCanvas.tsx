/**
 * WorkflowCanvas — a pre-wired `<ReactFlow>` for visualising a jcode run.
 *
 * Opinionated defaults (Vercel AI-Elements-style): dotted background tinted
 * with `--jcode-color-border`, `fitView`, `panOnScroll`, double-click zoom off.
 * The `jcodeStep` node type and the `jcodeAnimated` / `jcodeTemporary` edge
 * types are registered automatically; consumers can extend them via the
 * `nodeTypes` / `edgeTypes` props (merged over the defaults).
 *
 * Dark mode follows the design tokens — every colour resolves from
 * `[data-jcode-ui]` scope, so no `.dark` rules are needed here.
 *
 * Every other `<ReactFlow>` prop (nodes / edges / onNodesChange / onConnect …)
 * is passed straight through. Set `interactive={false}` for a read-only view.
 *
 * NOTE: import the base React Flow stylesheet once in your app *before* our
 * overrides:
 *
 *     import '@xyflow/react/dist/style.css'
 *     import 'jcode-ui/canvas.css'
 *
 * The root element must have an explicit height (React Flow requirement); this
 * wrapper defaults to `height: 100%; min-height: 320px`.
 */

import { memo } from 'react'
import type { ReactNode } from 'react'
import { ReactFlow, Background, BackgroundVariant } from '@xyflow/react'
import type { EdgeTypes, NodeTypes, ReactFlowProps } from '@xyflow/react'
import { jcodeNodeTypes } from './WorkflowNode.js'
import { jcodeEdgeTypes } from './WorkflowEdge.js'

export interface WorkflowCanvasProps extends ReactFlowProps {
  /** When false, disables node dragging / connecting / selection and pan-on-drag. */
  interactive?: boolean
  /** Render the dotted background layer (default true). */
  showBackground?: boolean
  children?: ReactNode
}

export const WorkflowCanvas = memo(function WorkflowCanvas({
  interactive = true,
  showBackground = true,
  nodeTypes,
  edgeTypes,
  className,
  fitView = true,
  panOnScroll = true,
  children,
  ...rest
}: WorkflowCanvasProps) {
  const mergedNodeTypes: NodeTypes = nodeTypes
    ? { ...jcodeNodeTypes, ...nodeTypes }
    : jcodeNodeTypes
  const mergedEdgeTypes: EdgeTypes = edgeTypes
    ? { ...jcodeEdgeTypes, ...edgeTypes }
    : jcodeEdgeTypes

  return (
    <div data-jcode-ui="" className={`jcode-wf-canvas ${className ?? ''}`.trimEnd()}>
      <ReactFlow
        nodeTypes={mergedNodeTypes}
        edgeTypes={mergedEdgeTypes}
        fitView={fitView}
        panOnScroll={panOnScroll}
        zoomOnDoubleClick={false}
        proOptions={{ hideAttribution: true }}
        {...rest}
        /* interactive is the component's contract — keep it after the spread
         * so a stray panOnDrag/nodesDraggable in rest can't silently undo it.
         * Consumers who want per-flag control pass interactive plus the flag
         * they want to differ. */
        panOnDrag={rest.panOnDrag ?? interactive}
        nodesDraggable={rest.nodesDraggable ?? interactive}
        nodesConnectable={rest.nodesConnectable ?? interactive}
        elementsSelectable={rest.elementsSelectable ?? interactive}
      >
        {showBackground ? (
          <Background
            variant={BackgroundVariant.Dots}
            gap={20}
            size={1}
            color="var(--jcode-color-border)"
          />
        ) : null}
        {children}
      </ReactFlow>
    </div>
  )
})
