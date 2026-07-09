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
  submitAskUser,
  editMessage,
  chatActions,
} from './store'
import type { ChatImage, AskUserAnswer } from 'jcode-ui-core'

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
        queued: s.chat.queued,
      }),
      actions: {
        sendMessage: (text, images) => store.dispatch(sendMessage({ text, images: images as ChatImage[] | undefined })),
        enqueueMessage: (text, images) =>
          store.dispatch(chatActions.enqueueMessage({ id: `q_${Date.now()}`, text, images: images as ChatImage[] | undefined })),
        removeQueuedMessage: (id) => store.dispatch(chatActions.removeQueued(id)),
        stop: () => store.dispatch(stopAgent()),
        resolveApproval: (id, approved, approveAll) =>
          store.dispatch(resolveApproval({ id, approved, approveAll })),
        submitAskUser: (id, answers) => store.dispatch(submitAskUser({ id, answers: answers as AskUserAnswer[] })),
        editMessage: (id, newText) => store.dispatch(editMessage({ id, text: newText })),
      },
    })
  }, [])
}
