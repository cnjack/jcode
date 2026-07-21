/**
 * ProviderIcon — inline SVG brand mark for a model provider, with an
 * initial-letter fallback. The SVG itself is host-supplied via `resolveIcon`
 * (the package cannot bundle `?raw` svg imports; the jcode app passes its
 * @lobehub/icons-static-svg lookup through `ProductComposerHost`).
 */

interface ProviderIconProps {
  provider: string
  size?: number
  custom?: boolean
  resolveIcon?: (provider: string, custom?: boolean) => string | null
}

export function ProviderIcon({ provider, size = 18, custom = false, resolveIcon }: ProviderIconProps) {
  const svg = resolveIcon?.(provider, custom) ?? null
  const initial = (provider || '').replace(/[^a-z0-9]/gi, '').charAt(0).toUpperCase() || '?'

  if (svg) {
    return (
      <span
        className="provider-icon"
        style={{ width: size, height: size, fontSize: size }}
        aria-hidden="true"
        dangerouslySetInnerHTML={{ __html: svg }}
      />
    )
  }

  return (
    <span
      className="provider-icon provider-icon-fallback"
      style={{ width: size, height: size, fontSize: Math.round(size * 0.52) }}
      aria-hidden="true"
    >
      {initial}
    </span>
  )
}
