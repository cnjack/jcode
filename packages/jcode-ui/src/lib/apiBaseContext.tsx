/**
 * ApiBaseContext — the host provides the API base URL (e.g. '' in browser mode
 * or 'http://127.0.0.1:<port>' in Tauri mode) so tool renderers that fetch
 * assets (browser_screenshot) can resolve image refs. Defaults to '' (same-origin).
 */

import { createContext } from 'react'

export const ApiBaseContext = createContext<string>('')

export interface ApiBaseProviderProps {
  /** API base URL with no trailing slash. */
  apiBase: string
  children: React.ReactNode
}

export function ApiBaseProvider({ apiBase, children }: ApiBaseProviderProps) {
  return <ApiBaseContext.Provider value={apiBase}>{children}</ApiBaseContext.Provider>
}
