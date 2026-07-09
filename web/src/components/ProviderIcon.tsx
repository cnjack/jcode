import { iconForProvider } from '../lib/providerIcons'

interface ProviderIconProps {
  provider: string
  size?: number
  custom?: boolean
}

export function ProviderIcon({ provider, size = 18, custom = false }: ProviderIconProps) {
  const svg = iconForProvider(provider, custom)
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
