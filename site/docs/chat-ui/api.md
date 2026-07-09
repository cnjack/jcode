---
title: API Reference
nav_order: 7
has_children: true
---

# API Reference

Programmatic surface of `jcode-ui` and `jcode-ui-core`. Prefer these pages for prop tables and type contracts; component docs focus on usage + live previews.

## Sections

| Page | Contents |
|------|----------|
| [Types](/chat-ui/docs/api/types) | `Message`, `ToolCall`, `Approval`, `ThreadItem`, … |
| [Runtime](/chat-ui/docs/api/runtime) | `ChatRuntime`, `createExternalStoreRuntime`, `createMockRuntime` |
| [Hooks](/chat-ui/docs/api/hooks) | Runtime hooks + behavioral hooks |
| [Primitives](/chat-ui/docs/api/primitives) | Headless components + slots |
| [Components](/chat-ui/docs/api/components) | Styled component props summary |
| [Generated API](/chat-ui/docs/api/generated) | Full symbol dump from TypeScript sources (auto-generated) |

Regenerate the dump after API changes:

```bash
node script/generate_jcode_ui_api_docs.mjs
# or: pnpm --dir packages/jcode-ui generate:api-docs
```

## Package entry points

```ts
// Styled + convenience re-exports
import { Thread, ChatInput, createExternalStoreRuntime, renderMarkdown } from 'jcode-ui'
import 'jcode-ui/styles.css'
import { TerminalRenderer } from 'jcode-ui/tool-renderers'

// Core (tree-shakeable)
import type { Message, ToolCall } from 'jcode-ui-core'
import { createMockRuntime } from 'jcode-ui-core/runtime'
import { Composer } from 'jcode-ui-core/primitives'
import { createToolRendererRegistry } from 'jcode-ui-core/adapters'
import { useAutoScroll } from 'jcode-ui-core/hooks'
```
