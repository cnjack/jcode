import { createI18n } from 'vue-i18n'
import en from './locales/en'

/**
 * i18n setup for the jcode web UI.
 *
 * Architecture borrows the locale-registry pattern from the sibling `jtype`
 * project (SUPPORTED_LOCALES / LOCALE_LABELS / HTML_LANG / getDefaultLocale),
 * adapted to vue-i18n's Composition-API mode (`legacy: false`).
 *
 * Persistence is front-end only: the chosen locale lives in localStorage under
 * `jcode_locale` (same `jcode_` prefix convention as `jcode_theme`). The backend
 * is intentionally unaware of the UI language — switching is instant and needs
 * no API round-trip. Messages for every locale are bundled (the catalog is
 * small enough that lazy-loading isn't worth the complexity).
 */

// The supported UI languages. `en` is the canonical source; the others are
// translations of it.
export const SUPPORTED_LOCALES = ['en', 'zh-Hans', 'zh-Hant', 'ja', 'ko'] as const
export type SupportedLocale = (typeof SUPPORTED_LOCALES)[number]

// Each locale's own endonym, shown in the language picker so the option is
// readable no matter which language the UI is currently in (a Japanese user
// can find 日本語 even while the UI is in English).
export const LOCALE_LABELS: Record<SupportedLocale, string> = {
  en: 'English',
  'zh-Hans': '简体中文',
  'zh-Hant': '繁體中文',
  ja: '日本語',
  ko: '한국어',
}

// BCP-47 tags for the <html lang> attribute. Keeping it in sync with the active
// locale fixes screen-reader pronunciation, browser translation prompts, and
// search-engine language detection.
const HTML_LANG: Record<SupportedLocale, string> = {
  en: 'en',
  'zh-Hans': 'zh-Hans',
  'zh-Hant': 'zh-Hant',
  ja: 'ja',
  ko: 'ko',
}

const STORAGE_KEY = 'jcode_locale'
const FALLBACK: SupportedLocale = 'en'

function isSupported(value: string | null | undefined): value is SupportedLocale {
  return !!value && (SUPPORTED_LOCALES as readonly string[]).includes(value)
}

// localStorage first, then the browser language (matching only the primary
// subtag — `zh-TW` maps to `zh-Hant`, `en-GB` maps to `en`), then English.
export function getDefaultLocale(): SupportedLocale {
  if (typeof window === 'undefined') return FALLBACK
  const stored = localStorage.getItem(STORAGE_KEY)
  if (isSupported(stored)) return stored
  // zh maps to simplified by default; the traditional variants (zh-TW/zh-Hant/
  // zh-HK) map to zh-Hant. Everything else matches the primary subtag.
  const browserTags = navigator.languages?.length ? navigator.languages : [navigator.language]
  for (const tag of browserTags) {
    if (!tag) continue
    const lower = tag.toLowerCase()
    if (lower === 'zh' || lower.startsWith('zh-cn') || lower.startsWith('zh-sg') || lower.startsWith('zh-hans')) {
      return 'zh-Hans'
    }
    if (lower.startsWith('zh-tw') || lower.startsWith('zh-hk') || lower.startsWith('zh-mo') || lower.startsWith('zh-hant')) {
      return 'zh-Hant'
    }
    const primary = lower.split('-')[0]
    if (primary === 'ja') return 'ja'
    if (primary === 'ko') return 'ko'
    if (primary === 'en') return 'en'
  }
  return FALLBACK
}

function applyDocumentLang(locale: SupportedLocale) {
  if (typeof document !== 'undefined') {
    document.documentElement.lang = HTML_LANG[locale] ?? locale
  }
}

export const i18n = createI18n({
  legacy: false, // Composition API mode — required for useI18n() in <script setup>
  locale: getDefaultLocale(),
  fallbackLocale: FALLBACK,
  messages: {
    en,
  },
})

// Apply the <html lang> on load so it's correct before the first paint.
applyDocumentLang(i18n.global.locale.value as SupportedLocale)

// Switch the active locale at runtime: updates vue-i18n, the <html lang>
// attribute, and persists the choice for next launch. Returns a Promise so
// callers can await the message file load if we ever switch to lazy loading.
export async function setLocale(locale: SupportedLocale): Promise<void> {
  if (!isSupported(locale)) return
  // Lazy-load the locale's messages on first use; en is bundled above.
  if (!(locale in i18n.global.messages.value)) {
    const mod = await import(`./locales/${locale}.ts`)
    i18n.global.setLocaleMessage(locale, mod.default)
  }
  // vue-i18n v11 narrows the `locale` type from the keys present in `messages`
  // at creation time (only `en` initially), so the runtime assignment of other
  // supported locales needs an explicit cast.
  ;(i18n.global.locale as { value: string }).value = locale
  applyDocumentLang(locale)
  localStorage.setItem(STORAGE_KEY, locale)
}

// Pre-load the initial non-en locale's messages so the first render is already
// translated (avoids a flash of English for zh/ja/ko users).
const initial = getDefaultLocale()
if (initial !== FALLBACK) {
  void setLocale(initial)
}
