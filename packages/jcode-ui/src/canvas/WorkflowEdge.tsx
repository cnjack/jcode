/**
 * WorkflowEdge — two custom React Flow edge renderers.
 *
 *   - `jcodeAnimated`  → a flowing dashed bezier (live data-flow between steps).
 *   - `jcodeTemporary` → a static grey dashed bezier (conditional / provisional
 *                        paths, e.g. a branch not yet taken).
 *
 * Both are thin wrappers over `BaseEdge` + `getBezierPath`; the motion and
 * colour come from token-scoped rules in `./canvas.css`. Props are the base
 * `EdgeProps` so the components stay assignable to `EdgeTypes` without a cast.
 */

import { memo } from 'react'
import { BaseEdge, getBezierPath } from '@xyflow/react'
import type { EdgeProps, EdgeTypes } from '@xyflow/react'

export const WorkflowAnimatedEdge = memo(function WorkflowAnimatedEdge({
  sourceX,
  sourceY,
  targetX,
  targetY,
  sourcePosition,
  targetPosition,
  markerEnd,
  style,
}: EdgeProps) {
  const [path] = getBezierPath({
    sourceX,
    sourceY,
    targetX,
    targetY,
    sourcePosition,
    targetPosition,
  })
  return (
    <BaseEdge
      path={path}
      markerEnd={markerEnd}
      style={style}
      className="jcode-wf-edge jcode-wf-edge--animated"
    />
  )
})

export const WorkflowTemporaryEdge = memo(function WorkflowTemporaryEdge({
  sourceX,
  sourceY,
  targetX,
  targetY,
  sourcePosition,
  targetPosition,
  markerEnd,
  style,
}: EdgeProps) {
  const [path] = getBezierPath({
    sourceX,
    sourceY,
    targetX,
    targetY,
    sourcePosition,
    targetPosition,
  })
  return (
    <BaseEdge
      path={path}
      markerEnd={markerEnd}
      style={style}
      className="jcode-wf-edge jcode-wf-edge--temporary"
    />
  )
})

/** Edge-type registry entry for `<WorkflowCanvas>` (and manual `<ReactFlow>` use). */
export const jcodeEdgeTypes: EdgeTypes = {
  jcodeAnimated: WorkflowAnimatedEdge,
  jcodeTemporary: WorkflowTemporaryEdge,
}
