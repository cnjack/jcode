/**
 * jcode-ui/canvas — optional workflow-canvas subentry.
 *
 * A runtime-wired take on Vercel AI Elements' Workflow suite: turn a jcode
 * `ToolCall` tree into an interactive React Flow graph, themed entirely with
 * `--jcode-*` design tokens (light/dark automatic).
 *
 * Peer dependency (optional): `@xyflow/react`. Install it in the host app.
 *
 * Required stylesheets (import once, in this order):
 *
 *     import '@xyflow/react/dist/style.css'   // React Flow base styles
 *     import 'jcode-ui/canvas.css'            // our token-scoped overrides
 *
 * Quick start:
 *
 *     import { WorkflowCanvas, CanvasControls, toolTreeToGraph } from 'jcode-ui/canvas'
 *
 *     const { nodes, edges } = toolTreeToGraph(tools)
 *     <WorkflowCanvas nodes={nodes} edges={edges} interactive={false}>
 *       <CanvasControls />
 *     </WorkflowCanvas>
 */

export { WorkflowCanvas } from './WorkflowCanvas.js'
export type { WorkflowCanvasProps } from './WorkflowCanvas.js'

export { WorkflowNode, jcodeNodeTypes } from './WorkflowNode.js'
export type { JcodeStepData, JcodeStepNode, JcodeStepStatus } from './WorkflowNode.js'

export { WorkflowAnimatedEdge, WorkflowTemporaryEdge, jcodeEdgeTypes } from './WorkflowEdge.js'

export { CanvasControls } from './CanvasControls.js'
export type { CanvasControlsProps } from './CanvasControls.js'

export { CanvasPanel } from './CanvasPanel.js'
export type { CanvasPanelProps } from './CanvasPanel.js'

export { toolTreeToGraph } from './toolTreeToGraph.js'
export type { ToolGraph, ToolTreeToGraphOptions } from './toolTreeToGraph.js'
