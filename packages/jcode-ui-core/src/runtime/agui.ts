/**
 * AG-UI runtime adapter — public surface.
 *
 * `createAGUIRuntime({ url })` folds an AG-UI protocol event stream into a
 * `ChatRuntime`, letting any AG-UI backend (LangGraph, CrewAI, Mastra, …) drive
 * jcode-ui with no glue. See `aguiRuntime.ts` for the reducer and `aguiEvents.ts`
 * for the event model + default SSE transport. Split across files for size; this
 * module is the single import point.
 */

export { createAGUIRuntime } from './aguiRuntime.js'
export type { AGUIRuntime, AGUIRuntimeOptions } from './aguiRuntime.js'
export { createFetchTransport, parseSSEStream, AGUIEventType } from './aguiEvents.js'
export type {
  AGUIEvent,
  AGUITransport,
  AGUIRunInput,
  AGUIMessage,
  AGUIToolCall,
  AGUIPatchOp,
  AGUIRole,
} from './aguiEvents.js'
