---
title: Tool renderers
nav_order: 5
---

# Tool renderers

`ToolCallCard` doesn't know how to render any specific tool. It looks up a renderer by `tool.name`
in a `ToolRendererRegistry`. jcode-ui ships default renderers; you can override or extend with your
own.

## Live gallery

<div data-jcode-demo="tool-gallery"></div>

## Default registry

`createDefaultToolRegistry()` registers renderers for common jcode tools:

| Tool name(s) | Renderer | Shows |
|--------------|----------|-------|
| `execute` | terminal | `$ command` + stdout/stderr |
| `read`, `write` | file-viewer | line-numbered table |
| `edit`, `multi_edit` | diff | red/green line table |
| `grep` | search | `file:line:content` matches |
| `todowrite`, `todoread` | todo | status-icon task list |
| `load_skill` | skill | name + description card |
| `team_list` / `team_send_message` / `team_create` / `team_spawn` | team | members / messages |
| `browser_screenshot` | browser-shot | inline screenshot image |
| *(fallback)* | generic | args + output + error |

```tsx
import { ToolRegistryProvider, createDefaultToolRegistry } from 'jcode-ui'

const registry = createDefaultToolRegistry()

<ToolRegistryProvider registry={registry}>
  <Thread />
</ToolRegistryProvider>
```

## Writing a custom renderer

```tsx
import type { ToolRenderer } from 'jcode-ui-core/adapters'

const MyRenderer: ToolRenderer = ({ name, args, output, status }) => {
  return (
    <div>
      <h4>{name}</h4>
      <pre>{output}</pre>
    </div>
  )
}
```

```ts
import { createToolRendererRegistry } from 'jcode-ui-core/adapters'
import { GenericRenderer } from 'jcode-ui/tool-renderers'

const registry = createToolRendererRegistry()
registry.register('my_custom_tool', MyRenderer)
registry.setFallback(GenericRenderer)
```

Or extend the defaults:

```ts
const registry = createDefaultToolRegistry()
registry.register('my_custom_tool', MyRenderer)
```

## Importing individual renderers

```ts
import { TerminalRenderer, DiffRenderer, GenericRenderer } from 'jcode-ui/tool-renderers'
```

## Render prop contract

| Prop | Type | Notes |
|------|------|-------|
| `name` | `string` | logical tool name |
| `args` | `string` | raw args JSON |
| `output` | `string?` | raw output (omitted while running) |
| `displayOutput` | `string?` | clean output |
| `error` | `string?` | on failure |
| `status` | `'running' \| 'done' \| 'error'` | |
| `displayInfo` | `ToolDisplayInfo?` | pre-extracted title/subtitle/icon |
| `children` | `ToolCall[]?` | nested subagent calls |

Renderers are pure functions of these props — no store access, no side effects.

## Guide

Step-by-step: [Custom tool renderer](/chat-ui/docs/guides/custom-tool-renderer).
