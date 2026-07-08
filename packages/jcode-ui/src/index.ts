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
export { ToolCallCard } from './components/ToolCallCard.js'
export { ApprovalBanner } from './components/ApprovalBanner.js'
export { AskUserCard } from './components/AskUserCard.js'
export { ContextBar } from './components/ContextBar.js'
export { ChatInput } from './components/ChatInput.js'

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
export { isMessageItem, isToolItem, isApprovalItem } from 'jcode-ui-core'
