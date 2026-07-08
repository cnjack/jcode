/**
 * ChannelsView — external messaging channels (WeChat / BLE).
 * Functional skeleton; full QR-login flow is a follow-up (lib/api.ts has the
 * channelLogin/logout/enable/disable + BLE endpoints wired).
 */

import { useEffect, useState } from 'react'
import { ChatBubbleOvalLeftIcon } from '@heroicons/react/24/outline'
import { api } from '../lib/api'

export function ChannelsView() {
  const [status, setStatus] = useState<{ available: boolean; channel?: string; state?: string } | null>(null)

  useEffect(() => {
    api.channelStatus().then(setStatus).catch(() => {})
  }, [])

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <header className="flex h-[var(--header-height)] shrink-0 items-center gap-2 border-b border-[var(--color-border)] bg-[var(--color-surface)] px-4">
        <ChatBubbleOvalLeftIcon className="h-4 w-4 text-[var(--color-primary)]" />
        <h1 className="text-sm font-medium">Channels</h1>
      </header>
      <div className="min-h-0 flex-1 overflow-y-auto p-4">
        <div className="max-w-md rounded-[var(--radius-lg)] border border-[var(--color-border)] bg-[var(--color-surface)] p-4">
          <div className="text-sm font-medium">WeChat</div>
          <div className="mt-1 text-xs text-[var(--color-muted-foreground)]">
            {status?.available ? `state: ${status.state ?? 'idle'}` : 'not available'}
          </div>
          <div className="mt-2 flex gap-2">
            <button
              type="button"
              onClick={() => api.channelEnable().catch(() => {})}
              className="rounded-[var(--radius-md)] bg-[var(--color-primary)] px-3 py-1 text-xs text-[var(--color-on-primary)]"
            >
              Enable
            </button>
            <button
              type="button"
              onClick={() => api.channelDisable().catch(() => {})}
              className="rounded-[var(--radius-md)] bg-[var(--color-muted)] px-3 py-1 text-xs"
            >
              Disable
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}
