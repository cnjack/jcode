import { describe, expect, it } from 'vitest'
import en from './locales/en'
import ja from './locales/ja'
import ko from './locales/ko'
import zhHans from './locales/zh-Hans'
import zhHant from './locales/zh-Hant'

function leafKeys(value: unknown, prefix = ''): string[] {
  if (!value || typeof value !== 'object') return [prefix]
  return Object.entries(value as Record<string, unknown>)
    .flatMap(([key, child]) => leafKeys(child, prefix ? `${prefix}.${key}` : key))
    .sort()
}

describe('conversation-loading translations', () => {
  it('keeps the dedicated page complete in all five locales', () => {
    const expected = leafKeys(en.conversationLoading)
    for (const locale of [zhHans, zhHant, ja, ko]) {
      expect(leafKeys(locale.conversationLoading)).toEqual(expected)
    }
  })

  it('keeps remote recovery status complete in all five locales', () => {
    const expected = leafKeys(en.remoteConnection)
    for (const locale of [zhHans, zhHant, ja, ko]) {
      expect(leafKeys(locale.remoteConnection)).toEqual(expected)
    }
  })
})
