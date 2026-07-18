/**
 * ThemeToggle — a compact dropdown that lists every built-in theme plus
 * 'System', plus a quick dark/light flip. Mirrors the Vue Sidebar's footer
 * toggle (which flips jcode-dark ↔ jcode-light) but surfaces the full list in
 * a popover rather than only in the Settings > Appearance tab.
 *
 * Styled with Tailwind + var(--color-*) tokens so it adapts to every theme
 * without per-theme classes.
 */
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { SunIcon, MoonIcon, SwatchIcon } from '@heroicons/react/24/outline'
import { useTheme, THEME_CHOICES } from '../lib/useTheme'

export function ThemeToggle({ compact = false }: { compact?: boolean }) {
  const { t } = useTranslation()
  const { theme, resolvedTheme, setTheme, toggleDark } = useTheme()
  const [open, setOpen] = useState(false)
  const rootRef = useRef<HTMLDivElement | null>(null)

  // Close on outside click / Esc. The toggle is small enough that this is the
  // only piece of focus-management it needs.
  useEffect(() => {
    if (!open) return
    function onPointerDown(e: MouseEvent) {
      if (!rootRef.current?.contains(e.target as Node)) setOpen(false)
    }
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') setOpen(false)
    }
    document.addEventListener('mousedown', onPointerDown)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('mousedown', onPointerDown)
      document.removeEventListener('keydown', onKey)
    }
  }, [open])

  return (
    <div ref={rootRef} className="relative inline-flex">
      {/* Quick flip — mirrors the Vue Sidebar footer button. The icon hints at
          the target state: SunIcon when dark ("switch to light"), MoonIcon when
          light ("switch to dark"). Long-press / dropdown caret via the adjacent
          SwatchIcon button. */}
      <button
        type="button"
        onClick={compact ? toggleDark : () => setOpen((v) => !v)}
        title={resolvedTheme === 'dark' ? t('nav.switchToLight') : t('nav.switchToDark')}
        aria-label={resolvedTheme === 'dark' ? t('nav.switchToLight') : t('nav.switchToDark')}
        className={`flex items-center justify-center rounded-[var(--radius-md)] text-[var(--color-muted-foreground)] transition-colors hover:bg-[var(--neutral-wash-soft)] hover:text-[var(--color-foreground)] ${compact ? 'h-9 w-9 hover:bg-[var(--color-muted)]' : 'h-[34px] w-[34px] hover:bg-[var(--color-muted)]'}`}
      >
        {resolvedTheme === 'dark' ? (
          <SunIcon className="h-[18px] w-[18px]" />
        ) : (
          <MoonIcon className="h-[18px] w-[18px]" />
        )}
      </button>

      {/* Picker toggle — opens the full theme list. Hidden in compact (sidebar footer) mode. */}
      {!compact && (
        <button
          type="button"
          onClick={() => setOpen((v) => !v)}
          title={t('nav.toggleTheme')}
          aria-label={t('nav.toggleTheme')}
          aria-haspopup="menu"
          aria-expanded={open}
          className="flex h-[34px] w-[34px] items-center justify-center rounded-[var(--radius-md)] text-[var(--color-muted-foreground)] transition-colors hover:bg-[var(--color-muted)] hover:text-[var(--color-foreground)]"
        >
          <SwatchIcon className="h-[18px] w-[18px]" />
        </button>
      )}

      {open && (
        <div
          role="menu"
          className="absolute right-0 top-full z-[var(--z-dropdown)] mt-1 min-w-[180px] rounded-[var(--radius-lg)] border border-[var(--color-border)] bg-[var(--color-surface)] p-1 shadow-[var(--shadow-md)]"
        >
          {THEME_CHOICES.map((t) => {
            const active = t.id === theme
            return (
              <button
                key={t.id}
                type="button"
                role="menuitemradio"
                aria-checked={active}
                onClick={() => {
                  setTheme(t.id)
                  setOpen(false)
                }}
                className="flex w-full items-center gap-2 rounded-[var(--radius-md)] px-2.5 py-1.5 text-left text-[12.5px] text-[var(--color-foreground)] transition-colors hover:bg-[var(--color-muted)]"
              >
                <span
                  className="inline-flex h-3.5 w-3.5 shrink-0 items-center justify-center"
                  aria-hidden="true"
                >
                  {active && <span className="h-1.5 w-1.5 rounded-full bg-[var(--color-accent-neutral)]" />}
                </span>
                <span className="flex-1 truncate">{t.label}</span>
              </button>
            )
          })}
        </div>
      )}
    </div>
  )
}
