/**
 * Theme management for the React product UI.
 *
 * Named built-in themes are applied via html[data-theme], with the `.dark`
 * class kept in sync with the active theme's appearance. A choice is either a
 * built-in theme id or the special 'system' value (follows OS preference).
 *
 * THEMES is generated from internal/theme/palette.go — never edit by hand.
 */
import { useSyncExternalStore } from 'react'
import { THEMES, type ThemeDef } from './themes.generated'

export type { ThemeDef }

/** A built-in named theme id, or 'system' to follow the OS preference. */
export type ThemeName = string

const STORAGE_KEY = 'jcode_theme'
const DEFAULT_DARK = 'jcode-dark'
const DEFAULT_LIGHT = 'jcode-light'

export { THEMES }

/** Theme picker options: the named themes plus the special 'system' value. */
export const THEME_CHOICES: { id: ThemeName; label: string }[] = [
  { id: 'system', label: 'System' },
  ...THEMES.map((t) => ({ id: t.id, label: t.label })),
]

function getSystemAppearance(): 'light' | 'dark' {
  if (typeof window === 'undefined' || !window.matchMedia) return 'dark'
  return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
}

function appearanceOf(id: string): 'light' | 'dark' {
  return THEMES.find((t) => t.id === id)?.appearance ?? 'dark'
}

// migrate maps legacy stored values ('light' | 'dark' | 'system') onto the new
// named-theme scheme. Newer values (a theme id or 'system') pass through.
function migrate(v: string | null): ThemeName {
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

function loadStored(): ThemeName {
  try {
    return migrate(localStorage.getItem(STORAGE_KEY))
  } catch {
    return 'system'
  }
}

// resolveId turns a choice into a concrete theme id.
function resolveId(choice: ThemeName): string {
  if (choice === 'system') {
    return getSystemAppearance() === 'dark' ? DEFAULT_DARK : DEFAULT_LIGHT
  }
  return THEMES.some((t) => t.id === choice) ? choice : DEFAULT_DARK
}

/** The currently-applied appearance (resolves 'system' against the OS). */
export function resolvedAppearance(choice: ThemeName): 'light' | 'dark' {
  return appearanceOf(resolveId(choice))
}

// ─── Module-level store ───────────────────────────────────────────────────
// A tiny external store so every useTheme() subscriber reads the same value.
let currentChoice: ThemeName = loadStored()
let currentResolved: 'light' | 'dark' = resolvedAppearance(currentChoice)
const listeners = new Set<() => void>()

function emit() {
  for (const l of listeners) l()
}

function subscribe(cb: () => void): () => void {
  listeners.add(cb)
  return () => listeners.delete(cb)
}

function snapshot(): ThemeName {
  return currentChoice
}

function resolvedSnapshot(): 'light' | 'dark' {
  return currentResolved
}

/**
 * Apply a theme choice to the document: set html[data-theme] and toggle the
 * `.dark` class to match the resolved appearance. Safe to call at the top level
 * (guards `document` for SSR/edge cases). Also syncs the browser chrome color
 * via the theme-color meta, matching the Vue implementation.
 */
export function applyTheme(choice: ThemeName): void {
  if (typeof document === 'undefined') return
  const id = resolveId(choice)
  const appearance = appearanceOf(id)
  currentResolved = appearance

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

// Apply once on module load so the very first paint matches the saved choice.
applyTheme(currentChoice)

// Re-resolve when the OS preference flips, but only if the user chose 'system'.
if (typeof window !== 'undefined' && window.matchMedia) {
  const mq = window.matchMedia('(prefers-color-scheme: dark)')
  mq.addEventListener('change', () => {
    if (currentChoice === 'system') {
      applyTheme('system')
      currentResolved = resolvedAppearance('system')
      emit()
    }
  })
}

export interface UseTheme {
  /** The saved choice ('system' or a theme id). */
  theme: ThemeName
  /** The currently applied appearance (resolves 'system' against the OS). */
  resolvedTheme: 'light' | 'dark'
  /** Persist a new choice and apply it. */
  setTheme: (choice: ThemeName) => void
  /** Quick flip between the brand dark/light defaults. */
  toggleDark: () => void
}

/**
 * Subscribe to theme state. Returns the saved choice, the resolved appearance,
 * and setters. Any component can call setTheme/toggleDark and every subscriber
 * re-renders with the new value.
 */
export function useTheme(): UseTheme {
  const theme = useSyncExternalStore(subscribe, snapshot, snapshot)
  const resolvedTheme = useSyncExternalStore(subscribe, resolvedSnapshot, resolvedSnapshot)

  function setTheme(choice: ThemeName): void {
    try {
      localStorage.setItem(STORAGE_KEY, choice)
    } catch {
      /* ignore */
    }
    currentChoice = choice
    applyTheme(choice)
    currentResolved = resolvedAppearance(choice)
    emit()
  }

  // toggleDark is the quick light/dark flip: switch to the brand default of
  // the opposite appearance. Named themes are chosen from the dropdown.
  function toggleDark(): void {
    setTheme(currentResolved === 'dark' ? DEFAULT_LIGHT : DEFAULT_DARK)
  }

  return { theme, resolvedTheme, setTheme, toggleDark }
}
