import { describe, expect, it } from 'vitest'
import { isAbsoluteLocalWorkspacePath } from './useDesktop'

describe('isAbsoluteLocalWorkspacePath', () => {
  it('accepts absolute macOS paths and rejects relative or remote workspaces', () => {
    expect(isAbsoluteLocalWorkspacePath('/Users/test/work/jcode')).toBe(true)
    expect(isAbsoluteLocalWorkspacePath('/Volumes/source/jcode')).toBe(true)
    expect(isAbsoluteLocalWorkspacePath('work/jcode')).toBe(false)
    expect(isAbsoluteLocalWorkspacePath('ssh://host/work/jcode')).toBe(false)
    expect(isAbsoluteLocalWorkspacePath('docker://container/work/jcode')).toBe(false)
  })
})
