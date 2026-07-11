/**
 * TestResultsRenderer — summarizes test-runner output. Recognizes:
 *   - Go:      `--- PASS/FAIL/SKIP: TestX (0.01s)` and `ok|FAIL pkg  0.02s`
 *   - Vitest / Jest: `✓ / ✗ name (2 ms)` and `Tests: N passed, N failed`
 *
 * Renders a semantic summary bar (passed · failed · skipped) plus a list of
 * failing cases with duration and an expandable error detail. When the output
 * isn't recognizable as test output, falls back to the raw text in a <pre>
 * (the registry has no second fallback once a renderer is chosen).
 *
 * Not registered in the default registry — a host maps it onto `execute`
 * conditionally (by inspecting args). `parseTestOutput` is exported for reuse.
 */

import { memo, useMemo, useState } from 'react'
import type { ToolRendererProps } from 'jcode-ui-core/adapters'

export interface TestCase {
  name: string
  status: 'pass' | 'fail' | 'skip'
  durationMs?: number
  detail?: string
}

export interface TestSummary {
  framework: 'go' | 'vitest' | 'jest' | 'unknown'
  passed: number
  failed: number
  skipped: number
  cases: TestCase[]
}

export const TestResultsRenderer = memo(function TestResultsRenderer({
  output,
  displayOutput,
  error,
  status,
}: ToolRendererProps) {
  const raw = displayOutput || output || ''
  const summary = useMemo(() => parseTestOutput(raw), [raw])

  if (!summary) {
    if (status === 'running') {
      return <div className="jcode-testresults__msg animate-pulse">Running tests…</div>
    }
    // Not recognizable — show the raw text so nothing is lost.
    const text = raw || error || ''
    return <pre className="jcode-testresults__raw">{text}</pre>
  }

  const failing = summary.cases.filter((c) => c.status === 'fail')

  return (
    <div data-jcode-ui="" className="jcode-testresults">
      <div className="jcode-testresults__bar" data-testid="testresults-bar">
        <span className="jcode-testresults__stat jcode-testresults__stat--pass">
          {summary.passed} passed
        </span>
        {summary.failed > 0 && (
          <>
            <span className="jcode-testresults__sep" aria-hidden>·</span>
            <span className="jcode-testresults__stat jcode-testresults__stat--fail">
              {summary.failed} failed
            </span>
          </>
        )}
        {summary.skipped > 0 && (
          <>
            <span className="jcode-testresults__sep" aria-hidden>·</span>
            <span className="jcode-testresults__stat jcode-testresults__stat--skip">
              {summary.skipped} skipped
            </span>
          </>
        )}
        {summary.framework !== 'unknown' && (
          <span className="jcode-testresults__framework">{summary.framework}</span>
        )}
      </div>

      {failing.length > 0 && (
        <ul className="jcode-testresults__list">
          {failing.map((c, i) => (
            <FailingCase key={`${c.name}-${i}`} test={c} />
          ))}
        </ul>
      )}
    </div>
  )
})

function FailingCase({ test }: { test: TestCase }) {
  const [open, setOpen] = useState(false)
  const hasDetail = !!test.detail
  return (
    <li className="jcode-testresults__case">
      <button
        type="button"
        className="jcode-testresults__case-row"
        onClick={() => hasDetail && setOpen((v) => !v)}
        aria-expanded={hasDetail ? open : undefined}
        disabled={!hasDetail}
      >
        <span className="jcode-testresults__case-icon" aria-hidden>✗</span>
        <span className="jcode-testresults__case-name">{test.name}</span>
        {typeof test.durationMs === 'number' && (
          <span className="jcode-testresults__case-dur">{formatMs(test.durationMs)}</span>
        )}
        {hasDetail && (
          <span className={`jcode-testresults__case-chev${open ? ' jcode-testresults__case-chev--open' : ''}`} aria-hidden>
            ▸
          </span>
        )}
      </button>
      {open && hasDetail && <pre className="jcode-testresults__detail">{test.detail}</pre>}
    </li>
  )
}

function formatMs(ms: number): string {
  if (ms < 1000) return `${Math.round(ms)}ms`
  return `${(ms / 1000).toFixed(2)}s`
}

const GO_CASE = /^\s*--- (PASS|FAIL|SKIP): (\S+) \(([\d.]+)s\)/
const GO_PKG = /^(ok|FAIL|PASS)\b(?:\s+(\S+))?(?:\s+([\d.]+)s)?/
const JS_PASS = /^\s*[✓✔√]\s+(.+?)(?:\s+\((\d+(?:\.\d+)?)\s*ms\))?\s*$/
const JS_FAIL = /^\s*[✗✘×✕]\s+(.+?)(?:\s+\((\d+(?:\.\d+)?)\s*ms\))?\s*$/
const JS_SUMMARY = /Tests?[:\s].*?(?:(\d+)\s+failed).*?(?:(\d+)\s+passed)|(\d+)\s+passed.*?(\d+)\s+failed/i
const SKIP_LINE = /^\s*(===|\?|PASS$|FAIL$)/

export function parseTestOutput(text: string): TestSummary | null {
  if (!text.trim()) return null
  const lines = text.split('\n')
  const cases: TestCase[] = []
  let framework: TestSummary['framework'] = 'unknown'
  let sawGoPkg = false
  let sawJsSummary = false

  let summaryPassed = 0
  let summaryFailed = 0

  let capturing: TestCase | null = null
  let detail: string[] = []
  const flush = () => {
    if (capturing && detail.length) {
      capturing.detail = detail.join('\n').replace(/\s+$/, '')
    }
    capturing = null
    detail = []
  }

  for (const line of lines) {
    const go = GO_CASE.exec(line)
    if (go) {
      flush()
      framework = 'go'
      const st = go[1] === 'PASS' ? 'pass' : go[1] === 'FAIL' ? 'fail' : 'skip'
      const c: TestCase = { name: go[2] ?? '', status: st, durationMs: parseFloat(go[3] ?? '0') * 1000 }
      cases.push(c)
      if (st === 'fail') capturing = c
      continue
    }

    const jsPass = JS_PASS.exec(line)
    if (jsPass && !line.includes('---')) {
      flush()
      if (framework === 'unknown') framework = 'vitest'
      cases.push({
        name: (jsPass[1] ?? '').trim(),
        status: 'pass',
        durationMs: jsPass[2] ? parseFloat(jsPass[2]) : undefined,
      })
      continue
    }
    const jsFail = JS_FAIL.exec(line)
    if (jsFail) {
      flush()
      if (framework === 'unknown') framework = 'vitest'
      const c: TestCase = {
        name: (jsFail[1] ?? '').trim(),
        status: 'fail',
        durationMs: jsFail[2] ? parseFloat(jsFail[2]) : undefined,
      }
      cases.push(c)
      capturing = c
      continue
    }

    const jsSum = JS_SUMMARY.exec(line)
    if (jsSum) {
      sawJsSummary = true
      if (framework === 'unknown') framework = /jest/i.test(text) ? 'jest' : 'vitest'
      const f = jsSum[1] ?? jsSum[4]
      const p = jsSum[2] ?? jsSum[3]
      if (f) summaryFailed = parseInt(f, 10)
      if (p) summaryPassed = parseInt(p, 10)
      continue
    }

    const goPkg = GO_PKG.exec(line)
    if (goPkg && (goPkg[1] === 'ok' || (goPkg[1] === 'FAIL' && goPkg[2]))) {
      sawGoPkg = true
      framework = 'go'
      flush()
      continue
    }

    // Detail capture for the current failing case.
    if (capturing) {
      if (SKIP_LINE.test(line)) {
        flush()
        continue
      }
      if (line.trim()) detail.push(line.replace(/^\t/, '  '))
    }
  }
  flush()

  if (cases.length === 0 && !sawGoPkg && !sawJsSummary) return null

  let passed = cases.filter((c) => c.status === 'pass').length
  let failed = cases.filter((c) => c.status === 'fail').length
  const skipped = cases.filter((c) => c.status === 'skip').length

  // Reconcile with a JS summary line when per-test lines were absent.
  if (cases.length === 0 && sawJsSummary) {
    passed = summaryPassed
    failed = summaryFailed
  }

  return { framework, passed, failed, skipped, cases }
}
