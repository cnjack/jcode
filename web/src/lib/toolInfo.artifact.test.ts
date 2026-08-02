import { describe, expect, it } from 'vitest'
import { extractToolDisplayInfo } from './toolInfo'

describe('show_artifact display info', () => {
  it('uses the title and falls back to a shortened path', () => {
    expect(extractToolDisplayInfo('show_artifact', JSON.stringify({
      path: 'reports/bench/result.html', title: 'Benchmark report',
    }))).toMatchObject({ title: 'artifact', subtitle: 'Benchmark report', icon: 'file' })
    expect(extractToolDisplayInfo('show_artifact', JSON.stringify({
      path: 'reports/bench/result.html',
    })).subtitle).toBe('…/bench/result.html')
  })
})
