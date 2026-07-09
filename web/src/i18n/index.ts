import i18n from 'i18next'
import { initReactI18next } from 'react-i18next'
import enBase from './locales/en'
import zhHansBase from './locales/zh-Hans'
import zhHantBase from './locales/zh-Hant'
import jaBase from './locales/ja'
import koBase from './locales/ko'

export const SUPPORTED_LOCALES = ['en', 'zh-Hans', 'zh-Hant', 'ja', 'ko'] as const
export type SupportedLocale = (typeof SUPPORTED_LOCALES)[number]

export const LOCALE_LABELS: Record<SupportedLocale, string> = {
  en: 'English',
  'zh-Hans': '简体中文',
  'zh-Hant': '繁體中文',
  ja: '日本語',
  ko: '한국어',
}

const STORAGE_KEY = 'jcode_locale'
const LEGACY_KEY = 'jcode-lang'
const FALLBACK: SupportedLocale = 'en'

const HTML_LANG: Record<SupportedLocale, string> = {
  en: 'en',
  'zh-Hans': 'zh-Hans',
  'zh-Hant': 'zh-Hant',
  ja: 'ja',
  ko: 'ko',
}

function isSupported(value: string | null | undefined): value is SupportedLocale {
  return !!value && (SUPPORTED_LOCALES as readonly string[]).includes(value)
}

function normalizeLegacy(value: string | null): SupportedLocale | null {
  if (!value) return null
  if (isSupported(value)) return value
  if (value === 'zh') return 'zh-Hans'
  return null
}

function browserLocale(): SupportedLocale {
  if (typeof navigator === 'undefined') return FALLBACK
  const tags = navigator.languages?.length ? navigator.languages : [navigator.language]
  for (const tag of tags) {
    const lower = tag.toLowerCase()
    if (lower === 'zh' || lower.startsWith('zh-cn') || lower.startsWith('zh-sg') || lower.startsWith('zh-hans')) return 'zh-Hans'
    if (lower.startsWith('zh-tw') || lower.startsWith('zh-hk') || lower.startsWith('zh-mo') || lower.startsWith('zh-hant')) return 'zh-Hant'
    const primary = lower.split('-')[0]
    if (primary === 'ja') return 'ja'
    if (primary === 'ko') return 'ko'
    if (primary === 'en') return 'en'
  }
  return FALLBACK
}

function initialLocale(): SupportedLocale {
  if (typeof localStorage === 'undefined') return FALLBACK
  return (
    normalizeLegacy(localStorage.getItem(STORAGE_KEY)) ||
    normalizeLegacy(localStorage.getItem(LEGACY_KEY)) ||
    browserLocale()
  )
}

function applyDocumentLang(locale: SupportedLocale) {
  if (typeof document !== 'undefined') {
    document.documentElement.lang = HTML_LANG[locale] ?? locale
  }
}

// Full locale files are the single source of truth (same shape as Vue's
// web/src/i18n/locales/*). Do NOT deep-merge incomplete partial overrides —
// those used to clobber keys like settings.backToWorkspace ("返回工作区" → "关闭").
const fullResources = {
  en: { translation: enBase },
  'zh-Hans': { translation: zhHansBase },
  'zh-Hant': { translation: zhHantBase },
  ja: { translation: jaBase },
  ko: { translation: koBase },
}

void i18n.use(initReactI18next).init({
  resources: fullResources,
  lng: initialLocale(),
  fallbackLng: FALLBACK,
  // Locale files use Vue-style `{n}` placeholders (not i18next's default `{{n}}`).
  interpolation: {
    escapeValue: false,
    prefix: '{',
    suffix: '}',
  },
})

applyDocumentLang(i18n.language as SupportedLocale)

export async function setLocale(locale: SupportedLocale): Promise<void> {
  if (!isSupported(locale)) return
  await i18n.changeLanguage(locale)
  applyDocumentLang(locale)
  localStorage.setItem(STORAGE_KEY, locale)
  localStorage.removeItem(LEGACY_KEY)
}

export { i18n }
