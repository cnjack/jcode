---
title: Canvas
parent: Components
nav_order: 20
---

# Canvas

An optional workflow-canvas suite (`jcode-ui/canvas`): turn a jcode `ToolCall` tree into an interactive React Flow graph, themed entirely with `--jcode-*` design tokens (light/dark automatic). A runtime-wired take on Vercel AI Elements' Workflow components.

> No live preview here — the canvas depends on the optional `@xyflow/react` peer, which the docs site doesn't bundle. The example below runs once you install it.

## Install

`@xyflow/react` is an **optional peer dependency** — install it in your host app:

```bash
npm install @xyflow/react
# jcode-ui declares it optional; you only pay for it if you use the canvas.
```

## Import

Import the base React Flow stylesheet **before** the jcode overrides, then the subentry:

```tsx
import '@xyflow/react/dist/style.css'   // React Flow base styles (first)
import 'jcode-ui/canvas.css'            // token-scoped overrides (second)

import { WorkflowCanvas, CanvasControls, toolTreeToGraph } from 'jcode-ui/canvas'
```

The canvas root needs an explicit height (a React Flow requirement); the wrapper defaults to `height: 100%; min-height: 320px`.

## Minimal example

```tsx
import '@xyflow/react/dist/style.css'
import 'jcode-ui/canvas.css'
import { WorkflowCanvas, CanvasControls, toolTreeToGraph } from 'jcode-ui/canvas'
import type { ToolCall } from 'jcode-ui-core'

function RunGraph({ tools }: { tools: ToolCall[] }) {
  const { nodes, edges } = toolTreeToGraph(tools)
  return (
    <div style={{ height: 420 }}>
      <WorkflowCanvas nodes={nodes} edges={edges} interactive={false}>
        <CanvasControls />
      </WorkflowCanvas>
    </div>
  )
}
```

`toolTreeToGraph` is pure and deterministic — safe to call on every stream tick (memoize on the tool array if it's large). A parent → child edge animates while the child is still running.

## Components

| Export | Purpose |
|--------|---------|
| `WorkflowCanvas` | Pre-wired `<ReactFlow>`: dotted token-tinted background, `fitView`, `panOnScroll`. Every other `ReactFlowProps` passes through. |
| `WorkflowNode`, `jcodeNodeTypes` | The `jcodeStep` node (title / subtitle / icon / status) + the node-types map. |
| `WorkflowAnimatedEdge`, `WorkflowTemporaryEdge`, `jcodeEdgeTypes` | Edge components + the edge-types map (animated = live data flow). |
| `CanvasControls` | Token-styled zoom / fit controls (floating `<Panel>`). |
| `CanvasPanel` | A freely-positioned overlay panel over the viewport. |
| `toolTreeToGraph` | Pure adapter: `ToolCall[]` → `{ nodes, edges }`. |

### `WorkflowCanvas` props

Extends `ReactFlowProps`. Notable additions:

| Prop | Type | Default | Notes |
|------|------|---------|-------|
| `interactive` | `boolean` | `true` | `false` disables dragging / connecting / selection / pan-on-drag (read-only view). |
| `showBackground` | `boolean` | `true` | Render the dotted background layer. |
| `nodeTypes` / `edgeTypes` | `NodeTypes` / `EdgeTypes` | jcode defaults | Merged **over** the built-in `jcodeStep` / `jcodeAnimated` / `jcodeTemporary` types. |

### `toolTreeToGraph` options

| Option | Type | Default | Notes |
|--------|------|---------|-------|
| `nodeWidth` | `number` | `220` | Fixed node width for horizontal spacing (matches CSS). |
| `levelGap` | `number` | `120` | Vertical distance between depth levels. |
| `siblingGap` | `number` | `40` | Horizontal gap between adjacent nodes. |

### `JcodeStepData`

```ts
type JcodeStepStatus = 'pending' | 'running' | 'done' | 'error'

type JcodeStepData = {
  title: string
  subtitle?: string
  icon?: ReactNode      // emoji / glyph / any node
  status?: JcodeStepStatus
}
```

## Related

- [ToolCallCard](/chat-ui/docs/components/tool-call-card) — the inline (non-graph) view
