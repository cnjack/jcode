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
| `Thread` | [Thread](/chat-ui/docs/components/thread) | Virtualized conversation timeline (+ `emptyState` / `suggestions` slots) |
| `ChatInput` | [ChatInput](/chat-ui/docs/components/chat-input) | Composer (send / queue / stop / attachments / dictation / slots) |
| `Message` | [Message](/chat-ui/docs/components/message) | User / assistant / system message (+ branch / feedback / retry / slots) |
| `ToolCallCard` | [ToolCallCard](/chat-ui/docs/components/tool-call-card) | Tool shell + renderer dispatch (+ slots) |
| `ApprovalBanner` | [ApprovalBanner](/chat-ui/docs/components/approval-banner) | Human approval gate (boolean or host-defined options) |
| `AskUserCard` | [AskUserCard](/chat-ui/docs/components/ask-user-card) | Mid-run questions |
| `Reasoning` | [Reasoning](/chat-ui/docs/components/reasoning) | Collapsible chain-of-thought |
| `Sources` | [Sources](/chat-ui/docs/components/sources) | Citation chips |
| `Attachment` | [Attachment](/chat-ui/docs/components/attachment) | Image thumbnails + pending upload chips |
| `ContextBar` | [ContextBar](/chat-ui/docs/components/context-bar) | Token / context ring |

### New in 0.2

| Component | Role |
|-----------|------|
| `ThreadWelcome` | Empty-thread hero (logo slot + title + starters) |
| `Suggestions` | Prompt pills — starters and follow-ups |
| `BranchPicker` | `‹ 2/3 ›` message-version navigation |
| `ConnectionBanner` | Sticky reconnecting / disconnected status |
| `ExportButton` | Download the conversation as markdown |
| `QuoteSelection` | Select thread text → quote into the composer |
| `ModelSelector` | Searchable, provider-grouped model picker |
| `ThreadList` | Sidebar thread list over the `ThreadStore` contract |
| `TaskList` / `RuntimeTaskList` | Todo/plan list (also powers the todo renderer) |
| `Artifact` | Container card for generated files/content |
| FileTree / TestResults / StackTrace | Runtime-wired tool renderers (`jcode-ui/tool-renderers`) |
| `jcode-ui/canvas` | Workflow canvas subentry (`@xyflow/react` peer) |
| `jcode-ui/voice` | SpeechInput / Transcription / AudioPlayer / VoiceVisualizer |

Providers: `RuntimeProvider`, `ToolRegistryProvider`, `ThreadStoreProvider`, `ApiBaseProvider` — see [Runtime](/chat-ui/docs/runtime).

## assistant-ui → jcode-ui mapping

| assistant-ui | jcode-ui equivalent | Status | Notes |
|--------------|---------------------|--------|-------|
| `Thread` | `Thread` | ✅ | Virtualized + auto-follow |
| `ThreadList` | `ThreadList` + `ThreadStore` | ✅ | Contract + styled sidebar |
| `ThreadWelcome` | `ThreadWelcome` | ✅ | |
| `Composer` | `ChatInput` | ✅ | Send/queue/stop, slash, attachments, drag/paste, dictation, slots |
| `Attachment` | `Attachment` + `AttachmentAdapter` | ✅ | Generic files + progress; image fast path |
| `Markdown` | built into `Message` | ✅ | Streaming-stable + block caching + code-block chrome |
| `Diff Viewer` | `DiffRenderer` | ✅ tool renderer | edit / multi_edit |
| `Image` | `Message` + `Attachment` | ✅ | |
| `Context Display` | `ContextBar` | ✅ | SVG ring + popover |
| `Message Timing` | `message.durationMs` | ✅ field | Footer on assistant messages |
| `Reasoning` | `Reasoning` | ✅ | |
| `Sources` | `Sources` | ✅ | |
| Follow-Up Suggestions | `Suggestions` | ✅ | Starters + follow-ups |
| Branch Picker | `BranchPicker` | ✅ | `Message.versions` + `switchVersion` |
| Feedback (👍👎) | built into `Message` | ✅ | `submitFeedback` action |
| Quote / SelectionToolbar | `QuoteSelection` | ✅ | → `ComposerHandle.insertText` |
| Export | `ExportButton` / `exportThreadMarkdown` | ✅ | |
| `Tool Fallback` | `GenericRenderer` | ✅ | Registry fallback |
| `Tool Group` | `tool.children` recursion + Exploring groups | ✅ | Subagent nesting |
| `ActionBar` (copy/edit) | built into `Message` | ✅ | Hover actions |
| Mermaid / LaTeX | `jcode-ui/plugins/mermaid` / `katex` | ✅ optional | Dynamic-import peers |
| Voice / Dictation | `jcode-ui/voice` + `enableDictation` | ✅ subentry | No 3D persona |
| `Model Selector` | `ModelSelector` | ✅ | Host supplies the model list |
| `Assistant Modal` | product app | 🟡 product | Build with primitives |
| AG-UI runtime | `createAGUIRuntime` | ✅ | Built into core |
| `makeAssistantToolUI` | `ToolRendererRegistry` | ✅ | |

**Legend:** ✅ library · 🟡 product app · field-driven = automatic from data.

## Full thread preview

<div data-jcode-demo="thread" data-height="420"></div>
