/**
 * Minimal ambient declarations for the optional peer plugins. We deliberately
 * do NOT add mermaid/katex as real dependencies — they are loaded via dynamic
 * `import()` only when the host opts in — so these shims keep `tsc` happy
 * without pulling the packages into the type graph.
 */

declare module 'mermaid' {
  export interface MermaidRenderResult {
    svg: string
    bindFunctions?: (element: Element) => void
  }
  export interface MermaidApi {
    initialize(config: Record<string, unknown>): void
    render(id: string, text: string, container?: Element): Promise<MermaidRenderResult>
    parse?(text: string): Promise<boolean> | boolean
  }
  const mermaid: MermaidApi
  export default mermaid
}

declare module 'katex' {
  export interface KatexOptions {
    displayMode?: boolean
    throwOnError?: boolean
    output?: 'html' | 'mathml' | 'htmlAndMathml'
    [key: string]: unknown
  }
  export interface KatexApi {
    renderToString(tex: string, options?: KatexOptions): string
  }
  const katex: KatexApi
  export default katex
}
