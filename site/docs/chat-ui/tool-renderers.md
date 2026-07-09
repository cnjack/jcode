---
title: Tool renderers
parent: jcode-ui
nav_order: 3
---

# Tool renderers

`ToolCallCard` doesn't know how to render any specific tool. It looks up a renderer by `tool.name`
in a `ToolRendererRegistry`. jcode-ui ships default renderers; you can override or extend with your
own. This is what makes the component reusable across agents with completely different tool
surfaces.

## The default registry

`createDefaultToolRegistry()` registers renderers for the common jcode tools:

| Tool name(s) | Renderer | Shows |
|--------------|----------|-------|
| `execute` | terminal | `$ command` + stdout/stderr |
| `read`, `write` | file-viewer | line-numbered table (`   N│content`) |
| `edit`, `multi_edit` | diff | red/green line table |
| `grep` | search | `file:line:content` matches |
| `todowrite`, `todoread` | todo | status-icon task list |
| `load_skill` | skill | name + description card |
| `team_list` / `team_send_message` / `team_create` | team | member tables / message cards |
| `browser_screenshot` | browser-shot | inline screenshot image |
| *(fallback)* | generic | args + output + error columns |

Provide it once at the app root via `<ToolRegistryProvider>`:

```tsx
import { ToolRegistryProvider, createDefaultToolRegistry } from 'jcode-ui'

const registry = createDefaultToolRegistry()

<ToolRegistryProvider registry={registry}>
  <Thread />
</ToolRegistryProvider>
```

## Writing a custom renderer

A renderer is a React component receiving `ToolRendererProps`:

```tsx
import type { ToolRenderer } from 'jcode-ui-core/adapters'

const MyRenderer: ToolRenderer = ({ name, args, output, status }) => {
  const data = JSON.parse(args)
  return (
    <div>
      <h4>{name}</h4>
      <pre>{output}</pre>
    </div>
  )
}
```

Register it:

```ts
import { createToolRendererRegistry } from 'jcode-ui-core/adapters'
import { GenericRenderer } from 'jcode-ui/tool-renderers'

const registry = createToolRendererRegistry()
registry.register('my_custom_tool', MyRenderer)
registry.setFallback(GenericRenderer)   // for tools without a specific renderer
```

You can also register against multiple names at once:

```ts
registry.register(['tool_a', 'tool_b'], SharedRenderer)
```

## Importing individual renderers

Each default renderer is individually importable so you can tree-shake or compose your own
registry:

```ts
import { TerminalRenderer, DiffRenderer, GenericRenderer } from 'jcode-ui/tool-renderers'
```

## Render prop contract

Every renderer receives:

| Prop | Type | Notes |
|------|------|-------|
| `name` | `string` | logical tool name |
| `args` | `string` | raw args JSON — parse what you need |
| `output` | `string?` | raw output (omitted while running) |
| `displayOutput` | `string?` | clean output (metadata stripped) |
| `error` | `string?` | error string on failure |
| `status` | `'running' \| 'done' \| 'error'` | drives shimmer / error tinting |
| `displayInfo` | `ToolDisplayInfo?` | pre-extracted title/subtitle/icon |
| `children` | `ToolCall[]?` | nested subagent calls — recurse if relevant |

Renderers are pure functions of these props — no store access, no side effects.
