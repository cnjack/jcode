/**
 * settings/atoms — shared class atoms + small primitives for the settings
 * view (M18). Extracted from the former SettingsDialog so sibling surfaces
 * (e.g. the Cloud tab in settings/) reuse the exact same `.s-*` design-token
 * styling instead of duplicating it.
 */

// ─── shared class atoms (mirror the Vue .s-* design tokens) ─────────────────

export const INPUT =
  'w-full h-8 px-2.5 rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-surface)] text-xs text-[var(--color-foreground)] outline-none focus:border-[var(--color-primary)] placeholder:text-[var(--color-muted-foreground)]'
export const INPUT_SM = INPUT + ' !h-7 text-[11px]'
export const INPUT_MONO = INPUT + ' font-mono'
export const TEXTAREA =
  'w-full min-h-[5rem] px-2.5 py-2 rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-surface)] text-xs text-[var(--color-foreground)] outline-none focus:border-[var(--color-primary)] placeholder:text-[var(--color-muted-foreground)] resize-y'
export const BTN =
  'inline-flex items-center justify-center gap-1.5 h-8 px-3 rounded-[var(--radius-md)] text-xs font-medium cursor-pointer border border-transparent transition-colors disabled:opacity-50 disabled:cursor-not-allowed'
export const BTN_PRIMARY = BTN + ' bg-[var(--color-primary)] text-[var(--color-on-primary)] hover:opacity-90'
export const BTN_SECONDARY =
  BTN + ' bg-[var(--color-surface)] border-[var(--color-border)] text-[var(--color-foreground)] hover:bg-[var(--color-secondary)]'
export const BTN_GHOST = BTN + ' bg-transparent text-[var(--color-foreground)] hover:bg-[var(--color-secondary)]'
export const BTN_DANGER = BTN + ' bg-[var(--color-destructive)] text-[var(--color-on-destructive)] hover:opacity-90'
export const BTN_SM = '!h-7 !px-2.5 !text-[11px] !rounded-[var(--radius-sm)]'
export const BTN_XS = '!h-[22px] !px-2 !text-[10px] !rounded-[var(--radius-sm)]'
export const ROW =
  'flex items-center gap-3 px-3.5 py-2.5 bg-[var(--color-surface)] border border-[var(--color-border)] rounded-[var(--radius-lg)]'
export const LABEL = 'block text-[11px] font-medium text-[var(--color-foreground)] mb-1.5'
export const CHIP =
  'inline-flex items-center gap-1 h-[18px] px-2 rounded-full text-[10px] font-medium bg-[var(--color-muted)] text-[var(--color-muted-foreground)] whitespace-nowrap'
export const CHIP_ACCENT = CHIP + ' !bg-[var(--neutral-wash)] !text-[var(--color-accent-neutral)]'
export const SECTION_TITLE = 'text-[13px] font-semibold tracking-tight text-[var(--color-foreground)]'

// ─── small shared components ────────────────────────────────────────────────

export function Switch({
  on,
  onClick,
  title,
  ariaLabel,
  disabled,
}: {
  on: boolean
  onClick: () => void
  title?: string
  ariaLabel?: string
  disabled?: boolean
}) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={on}
      aria-label={ariaLabel}
      disabled={disabled}
      onClick={onClick}
      title={title}
      className="relative h-5 w-[34px] shrink-0 rounded-full border-none p-0 transition-colors disabled:opacity-50"
      style={{ backgroundColor: on ? 'var(--color-accent-neutral)' : 'var(--color-border)' }}
    >
      <span
        className="absolute left-0.5 top-0.5 h-4 w-4 rounded-full bg-[var(--color-surface)] shadow-[var(--shadow-sm)] transition-transform"
        style={{ transform: on ? 'translateX(14px)' : 'translateX(0)' }}
      />
    </button>
  )
}

export function Segmented<T extends string>({
  value,
  options,
  onChange,
}: {
  value: T
  options: { value: T; label: string }[]
  onChange: (v: T) => void
}) {
  return (
    <div className="inline-flex gap-0.5 rounded-[var(--radius-md)] bg-[var(--color-muted)] p-0.5">
      {options.map((o) => (
        <button
          key={o.value}
          type="button"
          aria-pressed={value === o.value}
          onClick={() => onChange(o.value)}
          className="h-6 cursor-pointer rounded-[var(--radius-sm)] px-2.5 text-[11px] font-medium transition-colors"
          style={
            value === o.value
              ? { background: 'var(--color-surface)', color: 'var(--color-foreground)' }
              : { background: 'transparent', color: 'var(--color-muted-foreground)' }
          }
        >
          {o.label}
        </button>
      ))}
    </div>
  )
}

export function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="mb-3.5 last:mb-0">
      <label className={LABEL}>{label}</label>
      {children}
    </div>
  )
}

export function EmptyState({
  Icon,
  title,
  hint,
}: {
  Icon: React.ComponentType<{ className?: string }>
  title: string
  hint: string
}) {
  return (
    <div className="flex flex-col items-center justify-center gap-2.5 py-12 text-center">
      <div className="grid h-9 w-9 place-items-center rounded-lg bg-[var(--color-secondary)] text-[var(--color-muted-foreground)]">
        <Icon className="h-4 w-4" />
      </div>
      <div className="text-[13px] font-medium text-[var(--color-foreground)]">{title}</div>
      <div className="max-w-[240px] text-[11px] leading-relaxed text-[var(--color-muted-foreground)]">{hint}</div>
    </div>
  )
}
