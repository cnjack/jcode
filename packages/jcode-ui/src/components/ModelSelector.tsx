/**
 * ModelSelector — a pure, presentational model picker.
 *
 * Backend-agnostic: it takes a flat `models` list + `value` + `onChange` and
 * renders a trigger button (current label + chevron) with an upward popup menu
 * (search filter, grouped by provider, ✓ on the active model). No data
 * fetching, no runtime coupling — the host owns the catalog and the switch.
 *
 * Typical use is as the composer's `leadingControls`:
 *
 *   <ChatInput leadingControls={<ModelSelector models={models} value={id} onChange={switchModel} />} />
 *
 * Styling is token-driven (see `composer2.css`, `.jcode-model-selector`).
 */

import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { CheckIcon, ChevronUpDownIcon, MagnifyingGlassIcon } from '@heroicons/react/24/outline'

export interface ModelSelectorOption {
  id: string
  label: string
  /** Provider name — used for grouping and search. */
  provider?: string
  /** Optional one-line description shown under the label. */
  description?: string
}

export interface ModelSelectorProps {
  models: ModelSelectorOption[]
  /** Selected model id. */
  value?: string
  onChange: (id: string) => void
  disabled?: boolean
  /** Trigger label when nothing is selected. Default "Select model". */
  placeholder?: string
  className?: string
}

interface Group {
  provider: string
  options: ModelSelectorOption[]
}

export function ModelSelector({
  models,
  value,
  onChange,
  disabled = false,
  placeholder = 'Select model',
  className,
}: ModelSelectorProps) {
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState('')
  const [activeIndex, setActiveIndex] = useState(0)
  const rootRef = useRef<HTMLDivElement>(null)
  const searchRef = useRef<HTMLInputElement>(null)

  const selected = useMemo(() => models.find((m) => m.id === value), [models, value])

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase()
    if (!q) return models
    return models.filter(
      (m) =>
        m.label.toLowerCase().includes(q) ||
        m.id.toLowerCase().includes(q) ||
        (m.provider ?? '').toLowerCase().includes(q),
    )
  }, [models, query])

  // Stable grouping by provider, preserving first-seen order.
  const groups = useMemo<Group[]>(() => {
    const order: string[] = []
    const map = new Map<string, ModelSelectorOption[]>()
    for (const m of filtered) {
      const key = m.provider ?? ''
      if (!map.has(key)) {
        map.set(key, [])
        order.push(key)
      }
      map.get(key)!.push(m)
    }
    return order.map((provider) => ({ provider, options: map.get(provider)! }))
  }, [filtered])

  // Flat order matching visual order, for keyboard navigation.
  const flat = useMemo(() => groups.flatMap((g) => g.options), [groups])

  // Reset the active row whenever the filter set changes.
  useEffect(() => {
    setActiveIndex(0)
  }, [query, open])

  // Focus the search box on open.
  useLayoutEffect(() => {
    if (open) searchRef.current?.focus()
  }, [open])

  // Close on outside click / Escape.
  useEffect(() => {
    if (!open) return
    function onDocClick(e: MouseEvent) {
      if (rootRef.current && !rootRef.current.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', onDocClick)
    return () => document.removeEventListener('mousedown', onDocClick)
  }, [open])

  function choose(id: string) {
    onChange(id)
    setOpen(false)
    setQuery('')
  }

  function onSearchKeyDown(e: React.KeyboardEvent<HTMLInputElement>) {
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      setActiveIndex((i) => Math.min(i + 1, flat.length - 1))
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      setActiveIndex((i) => Math.max(i - 1, 0))
    } else if (e.key === 'Enter') {
      e.preventDefault()
      const opt = flat[activeIndex]
      if (opt) choose(opt.id)
    } else if (e.key === 'Escape') {
      e.preventDefault()
      setOpen(false)
    }
  }

  return (
    <div
      data-jcode-ui=""
      ref={rootRef}
      className={['jcode-model-selector', className].filter(Boolean).join(' ')}
    >
      <button
        type="button"
        className="jcode-model-selector__trigger"
        disabled={disabled}
        aria-haspopup="listbox"
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
      >
        <span className="jcode-model-selector__current">{selected ? selected.label : placeholder}</span>
        <ChevronUpDownIcon className="jcode-model-selector__chev" />
      </button>

      {open && (
        <div className="jcode-model-selector__menu" role="listbox">
          <div className="jcode-model-selector__search">
            <MagnifyingGlassIcon className="jcode-model-selector__search-icon" />
            <input
              ref={searchRef}
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              onKeyDown={onSearchKeyDown}
              placeholder="Search models…"
              aria-label="Search models"
            />
          </div>

          <div className="jcode-model-selector__list">
            {flat.length === 0 && <div className="jcode-model-selector__empty">No matching models</div>}
            {groups.map((g) => (
              <div key={g.provider || '__none'} className="jcode-model-selector__group">
                {g.provider && <div className="jcode-model-selector__group-label">{g.provider}</div>}
                {g.options.map((opt) => {
                  const flatIdx = flat.indexOf(opt)
                  const isActive = flatIdx === activeIndex
                  const isSelected = opt.id === value
                  return (
                    <button
                      key={opt.id}
                      type="button"
                      role="option"
                      aria-selected={isSelected}
                      onMouseEnter={() => setActiveIndex(flatIdx)}
                      onClick={() => choose(opt.id)}
                      className={`jcode-model-selector__option${isActive ? ' is-active' : ''}${
                        isSelected ? ' is-selected' : ''
                      }`}
                    >
                      <span className="jcode-model-selector__option-main">
                        <span className="jcode-model-selector__option-label">{opt.label}</span>
                        {opt.description && (
                          <span className="jcode-model-selector__option-desc">{opt.description}</span>
                        )}
                      </span>
                      {isSelected && <CheckIcon className="jcode-model-selector__check" />}
                    </button>
                  )
                })}
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  )
}
