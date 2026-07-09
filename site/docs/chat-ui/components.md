---
title: Components
nav_order: 4
has_children: true
---

# Component reference

Styled components exported by `jcode-ui`. Each page includes a **live preview** (real components, mock runtime) plus props tables.

## Catalog

| Component | Page | Role |
|-----------|------|------|
| `Thread` | [Thread](/chat-ui/docs/components/thread) | Virtualized conversation timeline |
| `ChatInput` | [ChatInput](/chat-ui/docs/components/chat-input) | Composer (send / queue / stop) |
| `Message` | [Message](/chat-ui/docs/components/message) | User / assistant / system bubble |
| `ToolCallCard` | [ToolCallCard](/chat-ui/docs/components/tool-call-card) | Tool shell + renderer dispatch |
| `ApprovalBanner` | [ApprovalBanner](/chat-ui/docs/components/approval-banner) | Human approval gate |
| `AskUserCard` | [AskUserCard](/chat-ui/docs/components/ask-user-card) | Mid-run questions |
| `Reasoning` | [Reasoning](/chat-ui/docs/components/reasoning) | Collapsible chain-of-thought |
| `Sources` | [Sources](/chat-ui/docs/components/sources) | Citation chips |
| `Attachment` | [Attachment](/chat-ui/docs/components/attachment) | Image thumbnails |
| `ContextBar` | [ContextBar](/chat-ui/docs/components/context-bar) | Token / context ring |

Providers: `RuntimeProvider`, `ToolRegistryProvider`, `ApiBaseProvider` — see [Runtime](/chat-ui/docs/runtime).

## assistant-ui → jcode-ui mapping

| assistant-ui | jcode-ui equivalent | Status | Notes |
|--------------|---------------------|--------|-------|
| `Thread` | `Thread` | ✅ | Virtualized + auto-follow |
| `ThreadList` | product `Sidebar` | 🟡 product | App chrome, not a library primitive |
| `Composer` | `ChatInput` | ✅ | Send/queue/stop, slash, attachments |
| `Attachment` | `Attachment` + `AttachmentList` | ✅ | Image-focused |
| `Markdown` | built into `Message` | ✅ | marked + highlight.js + DOMPurify |
| `Diff Viewer` | `DiffRenderer` | ✅ tool renderer | edit / multi_edit |
| `Image` | `Message` + `Attachment` | ✅ | |
| `Context Display` | `ContextBar` | ✅ | SVG ring + popover |
| `Message Timing` | `message.durationMs` | ✅ field | Footer on assistant messages |
| `Reasoning` | `Reasoning` | ✅ | |
| `Sources` | `Sources` | ✅ | |
| `Tool Fallback` | `GenericRenderer` | ✅ | Registry fallback |
| `Tool Group` | `tool.children` recursion | ✅ | Subagent nesting |
| `ActionBar` (copy/edit) | built into `Message` | ✅ | Hover actions |
| `Assistant Modal` | product app | 🟡 product | Build with primitives |
| `Model Selector` | product header | 🟡 product | Host-specific |
| Voice / Dictation | — | ❌ skipped | Not in scope |
| Branch Picker | — | ❌ | Not yet |
| `makeAssistantToolUI` | `ToolRendererRegistry` | ✅ | |

**Legend:** ✅ library · 🟡 product app · field-driven = automatic from data.

## Full thread preview

<div data-jcode-demo="thread" data-height="420"></div>
