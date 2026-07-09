---
title: Components API
parent: API Reference
nav_order: 5
---

# Components API (summary)

Styled exports from `jcode-ui`. Detailed docs + live previews live under [Components](/chat-ui/docs/components).

| Export | Key props |
|--------|-----------|
| `Thread` | `virtualize`, `overscanBottom`, `emptyState`, `renderPending`, `className` |
| `ChatInput` | `slashCommands`, `allowImages`, `placeholder`, `showContextBar`, `onSent` |
| `Message` | `message`, `canEdit` |
| `ToolCallCard` | `tool`, `registry?`, `className?` |
| `ApprovalBanner` | `approval` |
| `AskUserCard` | `tool` |
| `Reasoning` | `reasoning`, `defaultExpanded?`, `durationMs?` |
| `Sources` | `sources` |
| `Attachment` | `image`, `onRemove?`, `size?`, `preview?` |
| `AttachmentList` | `images`, `onRemove?`, `size?`, `preview?`, `className?` |
| `ContextBar` | `breakdown?`, `size?`, `dangerThreshold?`, `showPopover?` |
| `RuntimeProvider` | `runtime` |
| `ToolRegistryProvider` | `registry` |
| `ApiBaseProvider` | `apiBase` |
| `createDefaultToolRegistry` | `()` → registry with 9 defaults |
| `renderMarkdown` | `(text: string) => string` |

Type re-exports: `MessageData`, `ToolCall`, `Approval`, `ThreadItem`, `TokenSnapshot`, `TaskContextBreakdown`, `AskUserQuestion`, `AskUserAnswer`, `TodoItem`, `Goal`, `Role`, guards `isMessageItem` / `isToolItem` / `isApprovalItem`.
