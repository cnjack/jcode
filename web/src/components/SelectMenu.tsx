import {
  useEffect,
  useId,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type KeyboardEvent,
} from 'react'
import { CheckIcon, ChevronDownIcon } from '@heroicons/react/24/outline'

export interface SelectMenuOption {
  value: string
  label: string
  description?: string
}

interface SelectMenuProps {
  value: string
  options: SelectMenuOption[]
  onChange: (value: string) => void
  ariaLabel: string
  placeholder?: string
  disabled?: boolean
  size?: 'sm' | 'md'
  className?: string
}

/**
 * Token-driven select-only listbox for Tauri/WebView surfaces.
 * Native select popovers are owned by the OS and ignore the app theme.
 */
export function SelectMenu({
  value,
  options,
  onChange,
  ariaLabel,
  placeholder = '',
  disabled = false,
  size = 'md',
  className = '',
}: SelectMenuProps) {
  const [open, setOpen] = useState(false)
  const [activeIndex, setActiveIndex] = useState(0)
  const [opensUp, setOpensUp] = useState(false)
  const rootRef = useRef<HTMLDivElement>(null)
  const triggerRef = useRef<HTMLButtonElement>(null)
  const optionRefs = useRef<Array<HTMLButtonElement | null>>([])
  const menuId = `select-menu-${useId().replace(/:/g, '')}`
  const selectedIndex = useMemo(() => options.findIndex((option) => option.value === value), [options, value])
  const selected = selectedIndex >= 0 ? options[selectedIndex] : undefined

  useEffect(() => {
    if (!open) return
    function onPointerDown(event: PointerEvent) {
      if (rootRef.current && !rootRef.current.contains(event.target as Node)) setOpen(false)
    }
    document.addEventListener('pointerdown', onPointerDown)
    return () => document.removeEventListener('pointerdown', onPointerDown)
  }, [open])

  useEffect(() => {
    if (options.length === 0) setOpen(false)
    setActiveIndex((index) => Math.max(0, Math.min(index, options.length - 1)))
  }, [options.length])

  useLayoutEffect(() => {
    if (!open) return
    optionRefs.current[activeIndex]?.focus()
  }, [activeIndex, open])

  function openMenu(preferredIndex = selectedIndex >= 0 ? selectedIndex : 0) {
    if (disabled || options.length === 0) return
    const index = Math.max(0, Math.min(preferredIndex, options.length - 1))
    const rect = triggerRef.current?.getBoundingClientRect()
    const rowHeight = options.some((option) => option.description) ? 50 : 36
    const menuHeight = Math.min(options.length * rowHeight + 8, 224)
    setOpensUp(Boolean(rect && rect.bottom + menuHeight > window.innerHeight - 16 && rect.top > menuHeight + 16))
    setActiveIndex(index)
    setOpen(true)
  }

  function closeMenu(restoreFocus = false) {
    setOpen(false)
    if (restoreFocus) requestAnimationFrame(() => triggerRef.current?.focus())
  }

  function choose(index: number) {
    const option = options[index]
    if (!option) return
    onChange(option.value)
    closeMenu(true)
  }

  function onTriggerKeyDown(event: KeyboardEvent<HTMLButtonElement>) {
    if (event.key === 'ArrowDown') {
      event.preventDefault()
      openMenu(selectedIndex >= 0 ? selectedIndex : 0)
    } else if (event.key === 'ArrowUp') {
      event.preventDefault()
      openMenu(selectedIndex >= 0 ? selectedIndex : options.length - 1)
    } else if (event.key === 'Home') {
      event.preventDefault()
      openMenu(0)
    } else if (event.key === 'End') {
      event.preventDefault()
      openMenu(options.length - 1)
    }
  }

  function onListKeyDown(event: KeyboardEvent<HTMLDivElement>) {
    if (event.key === 'ArrowDown') {
      event.preventDefault()
      setActiveIndex((index) => Math.min(index + 1, options.length - 1))
    } else if (event.key === 'ArrowUp') {
      event.preventDefault()
      setActiveIndex((index) => Math.max(index - 1, 0))
    } else if (event.key === 'Home') {
      event.preventDefault()
      setActiveIndex(0)
    } else if (event.key === 'End') {
      event.preventDefault()
      setActiveIndex(options.length - 1)
    } else if (event.key === 'Enter' || event.key === ' ') {
      event.preventDefault()
      choose(activeIndex)
    } else if (event.key === 'Escape') {
      event.preventDefault()
      closeMenu(true)
    } else if (event.key === 'Tab') {
      setOpen(false)
    } else if (event.key.length === 1 && !event.metaKey && !event.ctrlKey && !event.altKey) {
      const query = event.key.toLocaleLowerCase()
      const next = options.findIndex(
        (option, index) => index > activeIndex && option.label.toLocaleLowerCase().startsWith(query),
      )
      const wrapped = options.findIndex((option) => option.label.toLocaleLowerCase().startsWith(query))
      const match = next >= 0 ? next : wrapped
      if (match >= 0) {
        event.preventDefault()
        setActiveIndex(match)
      }
    }
  }

  const triggerSize = size === 'sm' ? 'h-8 px-2 text-[12.5px]' : 'px-2.5 py-2 text-[13px]'

  return (
    <div
      ref={rootRef}
      className={`relative min-w-0 ${className}`}
      onBlur={(event) => {
        if (!event.currentTarget.contains(event.relatedTarget)) setOpen(false)
      }}
    >
      <button
        ref={triggerRef}
        type="button"
        disabled={disabled || options.length === 0}
        aria-label={ariaLabel}
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-controls={open ? menuId : undefined}
        onClick={() => (open ? closeMenu() : openMenu())}
        onKeyDown={onTriggerKeyDown}
        className={`flex w-full min-w-0 items-center gap-2 rounded-[var(--radius-lg)] border border-[var(--color-border)] bg-[var(--color-background)] text-left text-[var(--color-foreground)] outline-none transition-colors hover:border-[var(--color-muted-foreground)] focus-visible:border-[var(--color-primary)] disabled:cursor-not-allowed disabled:opacity-55 ${triggerSize}`}
      >
        <span className={`min-w-0 flex-1 truncate ${selected ? '' : 'text-[var(--color-muted-foreground)]'}`}>
          {selected?.label || placeholder}
        </span>
        <ChevronDownIcon
          aria-hidden="true"
          className={`h-3.5 w-3.5 shrink-0 text-[var(--color-muted-foreground)] transition-transform ${open ? 'rotate-180' : ''}`}
        />
      </button>

      {open && (
        <div
          id={menuId}
          role="listbox"
          aria-label={ariaLabel}
          onKeyDown={onListKeyDown}
          className={`absolute left-0 right-0 z-50 max-h-56 overflow-y-auto rounded-[var(--radius-lg)] border border-[var(--color-border)] bg-[var(--color-surface)] p-1 shadow-[var(--shadow-lg)] ${
            opensUp ? 'bottom-[calc(100%+4px)]' : 'top-[calc(100%+4px)]'
          }`}
        >
          {options.map((option, index) => {
            const isSelected = option.value === value
            const isActive = index === activeIndex
            return (
              <button
                key={option.value}
                id={`${menuId}-option-${index}`}
                ref={(node) => { optionRefs.current[index] = node }}
                type="button"
                role="option"
                aria-selected={isSelected}
                tabIndex={isActive ? 0 : -1}
                onPointerMove={() => setActiveIndex(index)}
                onClick={() => choose(index)}
                className={`flex w-full items-center gap-2 rounded-[var(--radius-md)] px-2.5 py-2 text-left text-[13px] text-[var(--color-foreground)] outline-none ${
                  isActive ? 'bg-[var(--color-muted)]' : 'bg-transparent'
                }`}
              >
                <span className="flex min-w-0 flex-1 flex-col">
                  <span className="truncate">{option.label}</span>
                  {option.description && (
                    <span className="truncate text-[11px] text-[var(--color-muted-foreground)]">
                      {option.description}
                    </span>
                  )}
                </span>
                {isSelected && <CheckIcon className="h-3.5 w-3.5 shrink-0 text-[var(--color-primary)]" />}
              </button>
            )
          })}
        </div>
      )}
    </div>
  )
}
