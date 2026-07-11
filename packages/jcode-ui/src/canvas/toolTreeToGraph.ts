/**
 * toolTreeToGraph — pure adapter from a jcode `ToolCall` tree to a React Flow
 * `{ nodes, edges }` graph for `<WorkflowCanvas>`.
 *
 * Layout is a lightweight tidy tree (no dagre): each depth level stacks
 * vertically (`levelGap`), leaves are laid out left-to-right (`nodeWidth`
 * + `siblingGap`), and every parent is centred over the span of its children.
 * A parent → child edge becomes `jcodeAnimated` when the child is still
 * running (live data flow), otherwise a plain bezier.
 *
 * Deterministic and side-effect free — safe to call on every render / stream
 * tick (memoise on the tool array if it is large).
 */

import type { ToolCall } from 'jcode-ui-core'
import type { Edge } from '@xyflow/react'
import type { JcodeStepData, JcodeStepNode } from './WorkflowNode.js'

export interface ToolTreeToGraphOptions {
  /** Fixed node width used for horizontal spacing (default 220, matches CSS). */
  nodeWidth?: number
  /** Vertical distance between depth levels (default 120). */
  levelGap?: number
  /** Horizontal gap between adjacent nodes (default 40). */
  siblingGap?: number
}

export interface ToolGraph {
  nodes: JcodeStepNode[]
  edges: Edge[]
}

export function toolTreeToGraph(
  tools: ToolCall[],
  options?: ToolTreeToGraphOptions,
): ToolGraph {
  const nodeWidth = options?.nodeWidth ?? 220
  const levelGap = options?.levelGap ?? 120
  const siblingGap = options?.siblingGap ?? 40
  const stepX = nodeWidth + siblingGap

  const nodes: JcodeStepNode[] = []
  const edges: Edge[] = []
  let leafCursor = 0

  /** Places `tool` (and its subtree), returns the tool's centre x. */
  function walk(tool: ToolCall, depth: number): number {
    const children = tool.children ?? []
    let x: number

    if (children.length === 0) {
      x = leafCursor * stepX
      leafCursor += 1
    } else {
      const childXs: number[] = []
      for (const child of children) {
        childXs.push(walk(child, depth + 1))
        const edge: Edge = {
          id: `${tool.id}->${child.id}`,
          source: tool.id,
          target: child.id,
        }
        if (child.status === 'running') edge.type = 'jcodeAnimated'
        edges.push(edge)
      }
      x = (childXs[0] + childXs[childXs.length - 1]) / 2
    }

    const data: JcodeStepData = {
      title: tool.displayInfo?.title ?? tool.name,
      subtitle: tool.displayInfo?.subtitle,
      icon: tool.displayInfo?.icon,
      status: tool.status,
    }
    nodes.push({
      id: tool.id,
      type: 'jcodeStep',
      position: { x, y: depth * levelGap },
      data,
    })
    return x
  }

  for (const root of tools) walk(root, 0)
  return { nodes, edges }
}
