// Theme management: named built-in themes applied via html[data-theme],
// with terminal/Go parity through the generated registry. A `.dark` class is
// kept in sync with the active theme's appearance so the existing
// :root:not(.dark) / .dark forks in style.css keep working.
import { ref, watch, onMounted } from 'vue'
import { THEMES, type ThemeDef } from './themes.generated'

export type { ThemeDef }

// A choice is either a built-in theme id or the special 'system' value, which
// follows the OS light/dark preference.
export type ThemeChoice = string

const STORAGE_KEY = 'jcode_theme'
const DEFAULT_DARK = 'jcode-dark'
const DEFAULT_LIGHT = 'jcode-light'

function getSystemAppearance(): 'light' | 'dark' {
  return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
}

function appearanceOf(id: string): 'light' | 'dark' {
  return THEMES.find((t) => t.id === id)?.appearance ?? 'dark'
}

// migrate maps legacy stored values ('light' | 'dark' | 'system') onto the new
// named-theme scheme. Newer values (a theme id or 'system') pass through.
function migrate(v: string | null): ThemeChoice {
  switch (v) {
    case null:
    case '':
    case 'system':
      return 'system'
    case 'light':
      return DEFAULT_LIGHT
    case 'dark':
      return DEFAULT_DARK
    default:
      return THEMES.some((t) => t.id === v) ? v : 'system'
  }
}

function loadStored(): ThemeChoice {
  try {
    return migrate(localStorage.getItem(STORAGE_KEY))
  } catch {
    return 'system'
  }
}

const themeChoice = ref<ThemeChoice>(loadStored())
const resolvedTheme = ref<'light' | 'dark'>('dark')

// resolveId turns a choice into a concrete theme id.
function resolveId(choice: ThemeChoice): string {
  if (choice === 'system') {
    return getSystemAppearance() === 'dark' ? DEFAULT_DARK : DEFAULT_LIGHT
  }
  return THEMES.some((t) => t.id === choice) ? choice : DEFAULT_DARK
}

function applyTheme(choice: ThemeChoice) {
  const id = resolveId(choice)
  const appearance = appearanceOf(id)
  resolvedTheme.value = appearance

  const html = document.documentElement
  html.setAttribute('data-theme', id)
  // Keep .dark in sync so style.css's scrollbar/prose/diff/xterm forks work.
  html.classList.toggle('dark', appearance === 'dark')

  // Sync the browser chrome color to the active theme background.
  const meta = document.querySelector('meta[name="theme-color"]')
  if (meta) {
    const bg = getComputedStyle(html).getPropertyValue('--color-background').trim()
    meta.setAttribute('content', bg || (appearance === 'dark' ? '#09090b' : '#fafafa'))
  }
}

export function useTheme() {
  function setTheme(choice: ThemeChoice) {
    themeChoice.value = choice
    try {
      localStorage.setItem(STORAGE_KEY, choice)
    } catch { /* ignore */ }
    applyTheme(choice)
  }

  // toggleTheme is the Sidebar's quick light/dark flip; it switches to the
  // brand default of the opposite appearance. The Appearance settings tab is
  // where specific named themes are chosen.
  function toggleTheme() {
    setTheme(resolvedTheme.value === 'dark' ? DEFAULT_LIGHT : DEFAULT_DARK)
  }

  onMounted(() => {
    applyTheme(themeChoice.value)

    const mq = window.matchMedia('(prefers-color-scheme: dark)')
    mq.addEventListener('change', () => {
      if (themeChoice.value === 'system') applyTheme('system')
    })
  })

  watch(themeChoice, (choice) => applyTheme(choice))

  return {
    themeChoice,
    resolvedTheme,
    themes: THEMES,
    setTheme,
    toggleTheme,
  }
}
