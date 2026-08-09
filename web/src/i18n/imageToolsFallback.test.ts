import { afterEach, describe, expect, it } from 'vitest'
import en from './locales/en'
import { i18n } from './index'

const fallbackLocales = ['zh-Hant', 'ja', 'ko'] as const

afterEach(async () => {
  await i18n.changeLanguage('en')
})

describe('image-tool locale fallback', () => {
  it.each(fallbackLocales)('uses complete English product copy for %s', (locale) => {
    const t = i18n.getFixedT(locale)

    expect(t('chat.model.imageOutput')).toBe(en.chat.model.imageOutput)
    expect(t('settings.providers.roles.imageDesc')).toBe(en.settings.providers.roles.imageDesc)
  })
})
