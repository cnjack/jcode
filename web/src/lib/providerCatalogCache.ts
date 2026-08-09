import type { CatalogModel, ProviderDetail } from './types'

const STORAGE_KEY = 'jcode.providerCatalogCache.v1'
const MAX_AGE_MS = 30 * 24 * 60 * 60 * 1000
const MAX_ENTRIES = 64

interface CatalogCacheEntry {
  provider_id: string
  updated_at: number
  models: CatalogModel[]
}

const memoryCache = new Map<string, CatalogCacheEntry>()
let hydrated = false

function cacheKey(provider: ProviderDetail): string {
  const accountID = provider.auth_binding?.account_id
    || provider.auth_status?.default_account_id
    || ''
  return [
    provider.id,
    provider.auth_binding?.method || 'api_key',
    accountID,
    provider.base_url || '',
  ].join('\u001f')
}

function cloneModels(models: CatalogModel[]): CatalogModel[] {
  return models.map((model) => ({
    ...model,
    effort_tiers: model.effort_tiers ? [...model.effort_tiers] : undefined,
  }))
}

function hydrate(): void {
  if (hydrated) return
  hydrated = true
  try {
    const raw = window.localStorage.getItem(STORAGE_KEY)
    if (!raw) return
    const parsed = JSON.parse(raw) as Record<string, CatalogCacheEntry>
    const now = Date.now()
    for (const [key, entry] of Object.entries(parsed)) {
      if (!entry || typeof entry.provider_id !== 'string'
        || typeof entry.updated_at !== 'number' || !Array.isArray(entry.models)
        || now - entry.updated_at > MAX_AGE_MS) continue
      memoryCache.set(key, {
        provider_id: entry.provider_id,
        updated_at: entry.updated_at,
        models: cloneModels(entry.models),
      })
    }
  } catch {
    // Cache corruption or unavailable storage must never block Settings.
  }
}

function persist(): void {
  try {
    const entries = [...memoryCache.entries()]
      .sort((left, right) => right[1].updated_at - left[1].updated_at)
      .slice(0, MAX_ENTRIES)
    memoryCache.clear()
    for (const [key, entry] of entries) memoryCache.set(key, entry)
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify(Object.fromEntries(entries)))
  } catch {
    // The in-memory cache remains usable when storage is disabled or full.
  }
}

export function readProviderCatalogCache(provider: ProviderDetail): CatalogModel[] | undefined {
  hydrate()
  const entry = memoryCache.get(cacheKey(provider))
  if (!entry || Date.now() - entry.updated_at > MAX_AGE_MS) return undefined
  return cloneModels(entry.models)
}

export function writeProviderCatalogCache(provider: ProviderDetail, models: CatalogModel[]): void {
  hydrate()
  memoryCache.set(cacheKey(provider), {
    provider_id: provider.id,
    updated_at: Date.now(),
    models: cloneModels(models),
  })
  persist()
}

export function removeProviderCatalogCache(providerID: string): void {
  hydrate()
  for (const [key, entry] of memoryCache) {
    if (entry.provider_id === providerID) memoryCache.delete(key)
  }
  persist()
}
