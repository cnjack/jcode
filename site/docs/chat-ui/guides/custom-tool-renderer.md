---
title: Custom tool renderer
parent: Guides
nav_order: 2
---

# Custom tool renderer

`ToolCallCard` looks up a React component by `tool.name`. Ship your own for agent-specific tools.

## 1. Write a renderer

```tsx
import type { ToolRenderer } from 'jcode-ui-core/adapters'

const DeployRenderer: ToolRenderer = ({ name, args, output, status, error }) => {
  const parsed = safeJson(args)
  return (
    <div className="p-3 text-sm">
      <div className="font-mono text-xs opacity-70">{name} · {status}</div>
      <div>env: {parsed?.env}</div>
      {error && <pre className="text-red-500">{error}</pre>}
      {output && <pre>{output}</pre>}
    </div>
  )
}

function safeJson(s: string) {
  try { return JSON.parse(s) } catch { return null }
}
```

## 2. Register

```ts
import { createDefaultToolRegistry } from 'jcode-ui'
// or start empty:
// import { createToolRendererRegistry } from 'jcode-ui-core/adapters'
// import { GenericRenderer } from 'jcode-ui/tool-renderers'

const registry = createDefaultToolRegistry()
registry.register('custom_deploy', DeployRenderer)
registry.register(['ship', 'deploy_prod'], DeployRenderer) // aliases
```

## 3. Provide

```tsx
<ToolRegistryProvider registry={registry}>
  <Thread />
</ToolRegistryProvider>
```

## Props contract

| Prop | Notes |
|------|-------|
| `name` | Logical tool name |
| `args` | Raw JSON string — parse what you need |
| `output` / `displayOutput` | May be absent while running |
| `error` | Failure string |
| `status` | `'running' \| 'done' \| 'error'` |
| `displayInfo` | Optional title/subtitle/icon from host |
| `children` | Nested subagent tools — recurse with `ToolCallCard` if useful |

Renderers should be **pure** — no store access, no side effects.

## Gallery of defaults

<div data-jcode-demo="tool-gallery"></div>
