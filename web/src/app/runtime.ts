/**
 * Runtime adapter — projects the RTK store into jcode-ui's RuntimeState and
 * binds the action bag. This is the single seam between the React app's state
 * layer and the reusable component library.
 *
 * Uses the module-level store singleton (the same one the <Provider> injects),
 * so actions can dispatch thunks directly. The runtime is stable for the app's
 * lifetime — memoized to a single instance.
 */

import { useMemo } from 'react'
import { createExternalStoreRuntime } from 'jcode-ui-core/runtime'
import type { ChatRuntime } from 'jcode-ui-core/runtime'
import { store } from './store'
import type { RootState } from './store'
import {
  sendMessage,
  stopAgent,
  resolveApproval,
  resolveApprovalOption,
  submitAskUser,
  editMessage,
  chatActions,
} from './store'
import type { ChatImage, AskUserAnswer, QueuedMessage } from 'jcode-ui-core'

/** Referentially-stable empty queue for sessions without a stash (keeps the
 *  runtime selector from re-rendering on every store change). */
const EMPTY_QUEUE: QueuedMessage[] = []

/** Build the ChatRuntime once. The store singleton is stable, so the runtime
 *  is too. */
export function useChatRuntime(): ChatRuntime {
  return useMemo(() => {
    return createExternalStoreRuntime<RootState>({
      // The EnhancedStore satisfies the { getState, subscribe } contract.
      store: store as unknown as {
        getState: () => RootState
        subscribe: (listener: () => void) => () => void
      },
      select: (s) => ({
        items: s.chat.timeline,
        isRunning: s.chat.isRunning,
        tokenSnapshot: s.chat.tokenSnapshot,
        goal: s.chat.goal,
        todos: s.chat.todos,
        // The composer shows only the FOREGROUND session's type-ahead queue;
        // other sessions' stashes stay in the store until their agentDone.
        queued: s.chat.queuedBySession[s.session.currentSessionId] ?? EMPTY_QUEUE,
      }),
      actions: {
        sendMessage: (text, images) => store.dispatch(sendMessage({ text, images: images as ChatImage[] | undefined })),
        enqueueMessage: (text, images) =>
          store.dispatch(chatActions.enqueueMessage({
            sessionId: store.getState().session.currentSessionId,
            message: { id: `q_${Date.now()}`, text, images: images as ChatImage[] | undefined },
          })),
        removeQueuedMessage: (id) =>
          store.dispatch(chatActions.removeQueued({ sessionId: store.getState().session.currentSessionId, id })),
        stop: () => store.dispatch(stopAgent()),
        resolveApproval: (id, approved, approveAll) =>
          store.dispatch(resolveApproval({ id, approved, approveAll })),
        resolveApprovalOption: (id, optionId) =>
          store.dispatch(resolveApprovalOption({ id, optionId })),
        submitAskUser: (id, answers) =>
          store.dispatch(submitAskUser({ id, answers: answers as AskUserAnswer[] })).unwrap(),
        editMessage: (id, newText) => store.dispatch(editMessage({ id, text: newText })),
      },
    })
  }, [])
}
