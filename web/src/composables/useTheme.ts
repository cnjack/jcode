// Theme management composable with system preference detection
import { ref, watch, onMounted } from 'vue'

export type ThemeMode = 'light' | 'dark' | 'system'

const STORAGE_KEY = 'jcode_theme'

function getSystemTheme(): 'light' | 'dark' {
  return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
}

function loadStoredTheme(): ThemeMode {
  try {
    const stored = localStorage.getItem(STORAGE_KEY)
    if (stored === 'light' || stored === 'dark' || stored === 'system') return stored
  } catch { /* ignore */ }
  return 'dark' // default to dark
}

const themeMode = ref<ThemeMode>(loadStoredTheme())
const resolvedTheme = ref<'light' | 'dark'>('dark')

function applyTheme(mode: ThemeMode) {
  const resolved = mode === 'system' ? getSystemTheme() : mode
  resolvedTheme.value = resolved

  const html = document.documentElement
  if (resolved === 'dark') {
    html.classList.add('dark')
  } else {
    html.classList.remove('dark')
  }

  // Update meta theme-color
  const meta = document.querySelector('meta[name="theme-color"]')
  if (meta) {
    meta.setAttribute('content', resolved === 'dark' ? '#09090b' : '#fafafa')
  }
}

export function useTheme() {
  function setTheme(mode: ThemeMode) {
    themeMode.value = mode
    localStorage.setItem(STORAGE_KEY, mode)
    applyTheme(mode)
  }

  function toggleTheme() {
    const next = resolvedTheme.value === 'dark' ? 'light' : 'dark'
    setTheme(next)
  }

  onMounted(() => {
    applyTheme(themeMode.value)

    // Listen for system preference changes
    const mq = window.matchMedia('(prefers-color-scheme: dark)')
    const handler = () => {
      if (themeMode.value === 'system') {
        applyTheme('system')
      }
    }
    mq.addEventListener('change', handler)
  })

  watch(themeMode, (mode) => applyTheme(mode))

  return {
    themeMode,
    resolvedTheme,
    setTheme,
    toggleTheme,
  }
}
