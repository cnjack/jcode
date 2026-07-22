import { CloudIcon, DevicePhoneMobileIcon, LockClosedIcon } from '@heroicons/react/24/outline'
import { useTranslation } from 'react-i18next'
import { CloudTab } from './settings/CloudTab'

/** Desktop's focused control surface for cloud access and paired mobile clients. */
export function CloudMobileView() {
  const { t } = useTranslation()

  return (
    <div className="page-surface flex min-h-0 flex-1 flex-col overflow-hidden">
      <header className="shrink-0 border-b border-[var(--color-border)] px-6 py-5">
        <div className="mx-auto flex max-w-4xl items-start gap-4">
          <div className="relative grid h-11 w-11 shrink-0 place-items-center rounded-[var(--radius-xl)] border border-[var(--color-border)] bg-[var(--color-surface)] text-[var(--color-accent-neutral)] shadow-[var(--shadow-sm)]">
            <CloudIcon className="h-5 w-5" />
            <span className="absolute -bottom-1 -right-1 grid h-5 w-5 place-items-center rounded-full border-2 border-[var(--color-background)] bg-[var(--color-foreground)] text-[var(--color-background)]">
              <DevicePhoneMobileIcon className="h-3 w-3" />
            </span>
          </div>
          <div className="min-w-0">
            <h1 className="text-[17px] font-semibold tracking-[-0.01em] text-[var(--color-foreground)]">
              {t('cloud.mobileHubTitle')}
            </h1>
            <p className="mt-1 max-w-2xl text-[12px] leading-relaxed text-[var(--color-muted-foreground)]">
              {t('cloud.mobileHubDesc')}
            </p>
          </div>
          <div className="ml-auto hidden items-center gap-1.5 rounded-full border border-[var(--color-border)] bg-[var(--color-muted)] px-2.5 py-1 text-[10.5px] text-[var(--color-muted-foreground)] sm:flex">
            <LockClosedIcon className="h-3 w-3" />
            {t('cloud.e2eeBadge')}
          </div>
        </div>
      </header>

      <div className="min-h-0 flex-1 overflow-y-auto px-6 py-6">
        <div className="mx-auto max-w-4xl rounded-[var(--radius-2xl)] border border-[var(--color-border)] bg-[var(--color-surface)] p-6 shadow-[var(--shadow-sm)]">
          <CloudTab heading={false} />
        </div>
      </div>
    </div>
  )
}
