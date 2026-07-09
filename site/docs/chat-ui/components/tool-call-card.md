---
title: ToolCallCard
parent: Components
nav_order: 4
---

# ToolCallCard

Expand/collapse shell for a tool invocation. Dispatches the body to a registered renderer. Recurses into `children` for subagent calls.

<div data-jcode-demo="tool-call"></div>

## Usage

```tsx
import { ToolCallCard } from 'jcode-ui'

<ToolCallCard tool={toolCall} />
```

## Props

| Prop | Type | Notes |
|------|------|-------|
| `tool` | `ToolCall` | Tool call data. |
| `registry` | `ToolRendererRegistry` | Optional override (defaults to context). |
| `className` | `string` | Passthrough. |

## Header chrome

- Status glyph: running / done / error  
- Title + subtitle from `displayInfo` (or extracted from args)  
- Diff counter (+N/-M) for edit tools  
- Shimmer while `status === 'running'`

## Renderer dispatch

```
tool.name  →  ToolRendererRegistry.get(name)  →  fallback GenericRenderer
```

Default registry (via `createDefaultToolRegistry()`):

| Tools | Renderer |
|-------|----------|
| `execute` | terminal |
| `read`, `write` | file-viewer |
| `edit`, `multi_edit` | diff |
| `grep` | search |
| `todowrite`, `todoread` | todo |
| `load_skill` | skill |
| team_* | team |
| `browser_screenshot` | browser-shot |
| * | generic |

See [Tool renderers](/chat-ui/docs/tool-renderers) and the [custom renderer guide](/chat-ui/docs/guides/custom-tool-renderer).

## Nested tools

When `tool.children` is set (subagent), cards nest recursively — the assistant-ui "Tool Group" pattern.

## Related

- [AskUserCard](/chat-ui/docs/components/ask-user-card) (rendered inside tools with `askUserQuestions`)  
- [ApprovalBanner](/chat-ui/docs/components/approval-banner)  
