import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '../lib/api'
import { productComposerActions } from './composerHost'
import { sessionActions, store } from './store'

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((done) => { resolve = done })
  return { promise, resolve }
}

beforeEach(() => {
  store.dispatch(sessionActions.setCurrentSession('task-a'))
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('composer git task scope', () => {
  it('targets the captured task and rejects stale branch results after navigation', async () => {
    const branches = deferred<Awaited<ReturnType<typeof api.gitBranches>>>()
    const checkout = deferred<Awaited<ReturnType<typeof api.gitCheckout>>>()
    const branchRequest = vi.spyOn(api, 'gitBranches').mockReturnValue(branches.promise)
    const checkoutRequest = vi.spyOn(api, 'gitCheckout').mockReturnValue(checkout.promise)

    const loadingBranches = productComposerActions.fetchBranches()
    const switching = productComposerActions.checkoutBranch('feature-a', false, '')
    await vi.waitFor(() => {
      expect(branchRequest).toHaveBeenCalledWith('task-a')
      expect(checkoutRequest).toHaveBeenCalledWith('feature-a', false, '', 'task-a')
    })

    store.dispatch(sessionActions.setCurrentSession('task-b'))
    branches.resolve({ current: 'main-a', branches: ['main-a'] })
    checkout.resolve({ branch: 'feature-a' })

    await expect(loadingBranches).rejects.toMatchObject({ name: 'AbortError' })
    await expect(switching).rejects.toMatchObject({ name: 'AbortError' })
  })
})
