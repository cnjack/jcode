/**
 * jcode-ui — styled React AI chat components, token-driven, backend-agnostic.
 *
 * Quick start:
 *
 *   import { RuntimeProvider, createExternalStoreRuntime, Thread, ChatInput } from 'jcode-ui'
 *   import 'jcode-ui/styles.css'
 *
 *   const runtime = createExternalStoreRuntime({ store, select, actions })
 *
 *   <RuntimeProvider runtime={runtime}>
 *     <Thread />
 *     <ChatInput />
 *   </RuntimeProvider>
 *
 * For custom tool renderers, see `jcode-ui/tool-renderers`.
 */

// Runtime + provider (re-exported from core for convenience).
export {
  RuntimeProvider,
  useRuntimeState,
  useRuntimeSelector,
  useRuntimeActions,
  createExternalStoreRuntime,
  createMockRuntime,
} from 'jcode-ui-core/runtime'

// Registry context + default registry builder.
export { ToolRegistryProvider, useToolRegistry, createDefaultToolRegistry } from './components/ToolRegistryContext.js'

// API base context (for browser_screenshot image resolution).
export { ApiBaseContext, ApiBaseProvider } from './lib/apiBaseContext.js'

// Styled components.
export { Thread } from './components/Thread.js'
export { Message } from './components/Message.js'
export { BranchPicker } from './components/BranchPicker.js'
export { ConnectionBanner } from './components/ConnectionBanner.js'
export { ToolCallCard } from './components/ToolCallCard.js'
export { ExploringGroupCard } from './components/ExploringGroupCard.js'
export { CompactToolRow } from './components/CompactToolRow.js'
export { ApprovalBanner } from './components/ApprovalBanner.js'
export { AskUserCard } from './components/AskUserCard.js'
export { ContextBar } from './components/ContextBar.js'
export { ChatInput } from './components/ChatInput.js'
export { ModelSelector } from './components/ModelSelector.js'
export type { ModelSelectorOption, ModelSelectorProps } from './components/ModelSelector.js'
export { Reasoning } from './components/Reasoning.js'
export { Sources } from './components/Sources.js'
export { Attachment, AttachmentList, PendingAttachmentList } from './components/Attachment.js'
export { ThreadList, formatRelative } from './components/ThreadList.js'
export { ThreadWelcome } from './components/ThreadWelcome.js'
export { Suggestions } from './components/Suggestions.js'
export type { SuggestionItem, SuggestionsProps } from './components/Suggestions.js'
export { ExportButton } from './components/ExportButton.js'
export { QuoteSelection, formatQuote } from './components/QuoteSelection.js'
export type { ToolCallCardProps, ToolCallCardSlots } from './components/ToolCallCard.js'
export { TaskList, RuntimeTaskList } from './components/TaskList.js'
export type {
  TaskListProps,
  TaskListItemProps,
  RuntimeTaskListProps,
} from './components/TaskList.js'
export { Artifact } from './components/Artifact.js'
export type { ArtifactProps } from './components/Artifact.js'

// Markdown pipeline (same renderer Message uses).
export { renderMarkdown } from './lib/markdown.js'

// Re-export the core types + primitives so consumers have a single import.
export type {
  Message as MessageData,
  ToolCall,
  Approval,
  ThreadItem,
  TokenSnapshot,
  TaskContextBreakdown,
  AskUserQuestion,
  AskUserAnswer,
  TodoItem,
  Goal,
  Role,
} from 'jcode-ui-core'
export {
  isMessageItem,
  isToolItem,
  isApprovalItem,
  isExploringItem,
  groupExploringTimeline,
  isCollapsibleTool,
  summarizeExploringSteps,
} from 'jcode-ui-core'
