// useFolderBrowser — shared local folder-browsing state for the two workspace
// pickers (the ProjectSwitcher modal and the inline WorkspacePicker popover),
// which previously each re-implemented identical browse/navigate logic against
// /api/browse. The composable owns the directory-list navigation; each caller
// wires its own "open this path" action (switch project vs. open folder).
import { ref } from 'vue'
import { api } from '@/composables/api'
import type { BrowseFolder } from '@/types/api'

export function useFolderBrowser() {
  const showBrowser = ref(false)
  const browsePath = ref('')
  const browseFolders = ref<BrowseFolder[]>([])
  const browseLoading = ref(false)
  const pathInput = ref('')

  async function loadFolders(path?: string) {
    browseLoading.value = true
    try {
      const result = await api.browse(path)
      browsePath.value = result.current
      pathInput.value = result.current
      browseFolders.value = result.folders
    } catch (err: unknown) {
      console.error('Browse failed:', err)
      browseFolders.value = []
    } finally {
      browseLoading.value = false
    }
  }

  function openBrowser() {
    showBrowser.value = true
    loadFolders()
  }

  function goUp() {
    if (!browsePath.value) return
    const parts = browsePath.value.split('/')
    parts.pop()
    loadFolders(parts.join('/') || '/')
  }

  function handlePathSubmit() {
    const path = pathInput.value.trim()
    if (path) loadFolders(path)
  }

  function resetBrowser() {
    showBrowser.value = false
    browsePath.value = ''
    pathInput.value = ''
    browseFolders.value = []
  }

  return {
    showBrowser,
    browsePath,
    browseFolders,
    browseLoading,
    pathInput,
    loadFolders,
    openBrowser,
    goUp,
    handlePathSubmit,
    resetBrowser,
  }
}
