let chatUIPagePromise: ReturnType<typeof importChatUIPage> | undefined

function importChatUIPage() {
  return import('../pages/ChatUIPage')
}

/** Share one import between React.lazy and intent-based navigation preloading. */
export function loadChatUIPage() {
  chatUIPagePromise ??= importChatUIPage()
  return chatUIPagePromise
}

export function preloadChatUIPage() {
  void loadChatUIPage()
}
