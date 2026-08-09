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

describe('provider managed-auth translations', () => {
  it('keeps all five locale resources structurally complete', () => {
    const expected = leafKeys(en.settings.providers.auth)
    for (const resource of [zhHans, zhHant, ja, ko]) {
      expect(leafKeys(resource.settings.providers.auth)).toEqual(expected)
    }
  })
})
