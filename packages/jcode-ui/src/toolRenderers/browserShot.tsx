/**
 * BrowserShotRenderer — renders `browser_screenshot` tool calls.
 * Extracts the image_ref emitted in output (`image_ref=/api/browser/shots/…`)
 * and renders it inline. The image URL must be resolved against the API base by
 * the host; here we accept an optional `apiBase` via context to prefix it.
 *
 * If no image_ref is present (still running, or a non-screenshot browser tool),
 * falls back to a generic block.
 */

import { memo, useContext, useMemo } from 'react'
import type { ToolRendererProps } from 'jcode-ui-core/adapters'
import { ApiBaseContext } from '../lib/apiBaseContext.js'
import { GenericRenderer } from './generic.js'

export const BrowserShotRenderer = memo(function BrowserShotRenderer(props: ToolRendererProps) {
  const apiBase = useContext(ApiBaseContext)
  const src = useMemo(() => {
    const m = (props.output ?? '').match(/image_ref=(\/api\/browser\/shots\/[\w-]+\.png)/)
    return m ? `${apiBase}${m[1]}` : ''
  }, [props.output, apiBase])
  if (!src) return <GenericRenderer {...props} />
  return (
    <div className="jcode-browser-shot my-1 overflow-hidden rounded-[var(--radius-md)] border border-[var(--color-border)]">
      <img src={src} alt="browser screenshot" className="max-h-[400px] w-full object-contain" />
    </div>
  )
})
