/**
 * ConnectionBanner — sticky transport-liveness strip.
 *
 * Reads `state.connection` (defaults to 'connected'). When connected it renders
 * nothing, except for a brief "Reconnected" success flash right after recovering
 * from a non-connected state (2s, tracked via an internal prev-value ref).
 *
 *   reconnecting → warning tint + spinning icon
 *   disconnected → error tint + explanatory copy
 *
 * role="status" / aria-live="polite" so assistive tech announces transitions
 * without stealing focus.
 */

import { memo, useEffect, useRef, useState } from 'react'
import { ArrowPathIcon, CheckCircleIcon, ExclamationTriangleIcon } from '@heroicons/react/24/outline'
import type { ConnectionState } from 'jcode-ui-core'
import { useRuntimeSelector } from 'jcode-ui-core/runtime'

const RECONNECTED_MS = 2000

export const ConnectionBanner = memo(function ConnectionBanner() {
  const connection = useRuntimeSelector((s) => s.connection)
  const prev = useRef<ConnectionState>(connection)
  const [flashRecovered, setFlashRecovered] = useState(false)

  useEffect(() => {
    const from = prev.current
    prev.current = connection
    if (connection === 'connected' && from !== 'connected') {
      // Just recovered — show the success flash, then let it fade.
      setFlashRecovered(true)
      const t = setTimeout(() => setFlashRecovered(false), RECONNECTED_MS)
      return () => clearTimeout(t)
    }
    if (connection !== 'connected' && flashRecovered) {
      // Dropped again before the flash expired — clear it immediately.
      setFlashRecovered(false)
    }
    return undefined
  }, [connection, flashRecovered])

  // Steady connected state with no pending flash → nothing to show.
  if (connection === 'connected' && !flashRecovered) return null

  const state: 'reconnecting' | 'disconnected' | 'reconnected' =
    connection === 'reconnecting'
      ? 'reconnecting'
      : connection === 'disconnected'
        ? 'disconnected'
        : 'reconnected'

  return (
    <div
      data-jcode-ui=""
      className="jcode-connection-banner"
      data-state={state}
      role="status"
      aria-live="polite"
    >
      {state === 'reconnecting' && (
        <>
          <ArrowPathIcon className="jcode-connection-icon animate-spin" aria-hidden />
          <span>Reconnecting…</span>
        </>
      )}
      {state === 'disconnected' && (
        <>
          <ExclamationTriangleIcon className="jcode-connection-icon" aria-hidden />
          <span>Disconnected</span>
          <span className="jcode-connection-detail">
            You’re offline — changes will resume once the connection is restored.
          </span>
        </>
      )}
      {state === 'reconnected' && (
        <>
          <CheckCircleIcon className="jcode-connection-icon" aria-hidden />
          <span>Reconnected</span>
        </>
      )}
    </div>
  )
})
