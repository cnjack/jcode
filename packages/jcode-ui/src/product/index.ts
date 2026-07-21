/**
 * jcode-ui/product — the jcode product composer, extracted from the desktop
 * app (web/) so other hosts (console/mobile) can ship the exact same input
 * experience:
 *
 *   import { ChatInput, GoalBanner } from 'jcode-ui/product'
 *
 *   <RuntimeProvider runtime={runtime}>
 *     <GoalBanner host={host} />
 *     <ChatInput host={host} onSent={...} />
 *   </RuntimeProvider>
 *
 * The components read the timeline/queue/token/goal state from the jcode-ui
 * ChatRuntime and everything product-specific (model catalog, slash commands,
 * workspace/branch pickers, i18n strings, provider icons, backend calls) from
 * the `ProductComposerHost` prop. Styling uses the product theme tokens
 * (--color-*, --radius-*, …) — the same custom properties the jcode web app
 * gets from tokens.generated.css, NOT the generic --jcode-* library tokens.
 */

export { ChatInput } from './ChatInput.js'
export type { ProductChatInputProps } from './ChatInput.js'
export { WorkspacePicker } from './WorkspacePicker.js'
export { BranchPicker } from './BranchPicker.js'
export { GoalBanner } from './GoalBanner.js'
export { ProviderIcon } from './ProviderIcon.js'

export type { ProductComposerHost } from './host.js'
export type { ProductComposerStrings } from './strings.js'
export { defaultProductComposerStrings, resolveProductComposerStrings } from './strings.js'
export { readDraft, writeDraft } from './drafts.js'
export { isRemotePath, parseRemoteLabel, workspaceName } from './remote.js'

export type {
  AgentMode,
  ReasoningOption,
  ModelInfo,
  ProviderInfo,
  ModelRef,
  SlashCommandInfo,
  TaskContextBreakdown,
  TaskStats,
  WorkspaceTaskRef,
  BrowseFolder,
  BrowseResult,
  GitBranchesResult,
  GitCheckoutResult,
  RemoteKind,
  RemoteMeta,
  RemotePrefill,
} from './types.js'
