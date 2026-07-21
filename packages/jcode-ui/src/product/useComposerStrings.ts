/**
 * useComposerStrings — merge host string overrides over the English defaults.
 */

import { useMemo } from 'react'
import type { ProductComposerHost } from './host.js'
import { resolveProductComposerStrings } from './strings.js'
import type { ProductComposerStrings } from './strings.js'

export function useComposerStrings(host: ProductComposerHost): ProductComposerStrings {
  return useMemo(() => resolveProductComposerStrings(host.strings), [host.strings])
}
