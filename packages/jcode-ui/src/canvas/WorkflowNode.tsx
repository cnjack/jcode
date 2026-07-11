/**
 * WorkflowNode — the custom `jcodeStep` React Flow node.
 *
 * A token-driven card (surface / radius-lg / shadow-sm) with an icon slot,
 * title, subtitle and a status affordance. Status drives the frame:
 *   - running → primary border, breathing pulse
 *   - error   → destructive border + tint
 *   - done / pending → neutral
 *
 * Props are typed as the base `NodeProps` (not `NodeProps<JcodeStepNode>`) so
 * the component stays assignable to React Flow's `NodeTypes` without a cast;
 * `data` is narrowed internally. Styling lives in `./canvas.css`.
 */

import { memo } from 'react'
import type { ReactNode } from 'react'
import { Handle, Position } from '@xyflow/react'
import type { Node, NodeProps, NodeTypes } from '@xyflow/react'

/** Lifecycle of a step; a superset of the runtime `ToolStatus`. */
export type JcodeStepStatus = 'pending' | 'running' | 'done' | 'error'

/**
 * Payload for a `jcodeStep` node. Declared as a type literal (not an
 * interface) so it satisfies React Flow's `Record<string, unknown>` node-data
 * constraint via an implicit index signature.
 */
export type JcodeStepData = {
  title: string
  subtitle?: string
  /** Icon slot — a string (emoji / glyph) or any React node. */
  icon?: ReactNode
  status?: JcodeStepStatus
}

/** Concrete node type for this renderer. */
export type JcodeStepNode = Node<JcodeStepData, 'jcodeStep'>

export const WorkflowNode = memo(function WorkflowNode({ data, selected }: NodeProps) {
  const d = data as JcodeStepData
  const status: JcodeStepStatus = d.status ?? 'pending'
  return (
    <div
      data-jcode-ui=""
      className={`jcode-wf-node jcode-wf-node--${status}${selected ? ' is-selected' : ''}`}
    >
      <Handle type="target" position={Position.Top} className="jcode-wf-node__handle" />
      {d.icon != null && d.icon !== '' ? (
        <span className="jcode-wf-node__icon" aria-hidden>
          {d.icon}
        </span>
      ) : null}
      <div className="jcode-wf-node__body">
        <div className="jcode-wf-node__title">{d.title}</div>
        {d.subtitle ? <div className="jcode-wf-node__subtitle">{d.subtitle}</div> : null}
      </div>
      <span className="jcode-wf-node__status" data-status={status} aria-hidden />
      <Handle type="source" position={Position.Bottom} className="jcode-wf-node__handle" />
    </div>
  )
})

/** Node-type registry entry for `<WorkflowCanvas>` (and manual `<ReactFlow>` use). */
export const jcodeNodeTypes: NodeTypes = { jcodeStep: WorkflowNode }
