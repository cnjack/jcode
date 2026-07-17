/**
 * ComputerShotRenderer — `computer_screenshot`.
 * Extracts image_ref from output and renders inline. Falls back to generic.
 *
 * Mirrors BrowserShotRenderer: the ref rides inside the tool's result text and
 * the image is fetched over HTTP (see internal/tools/computer.go).
 */

import { memo, useContext, useMemo } from 'react'
import type { ToolRendererProps } from 'jcode-ui-core/adapters'
import { ApiBaseContext } from '../lib/apiBaseContext.js'
import { GenericRenderer } from './generic.js'

export const ComputerShotRenderer = memo(function ComputerShotRenderer(props: ToolRendererProps) {
  const apiBase = useContext(ApiBaseContext)
  const src = useMemo(() => {
    const m = (props.output ?? '').match(/image_ref=(\/api\/computer\/shots\/[\w-]+\.png)/)
    return m ? `${apiBase}${m[1]}` : ''
  }, [props.output, apiBase])

  if (!src) return <GenericRenderer {...props} />

  return (
    <div className="jcode-computer-shot px-3 py-2" style={{ background: 'var(--jcode-color-surface)' }}>
      <a href={src} target="_blank" rel="noopener noreferrer">
        <img
          src={src}
          alt="app screenshot"
          className="max-h-80 max-w-full rounded-md border"
          style={{ borderColor: 'var(--jcode-color-border)' }}
        />
      </a>
    </div>
  )
})
