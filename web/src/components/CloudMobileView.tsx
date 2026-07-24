import {
  ArrowRightIcon,
  CheckIcon,
  CloudIcon,
  ComputerDesktopIcon,
  DevicePhoneMobileIcon,
  GlobeAltIcon,
  LockClosedIcon,
} from '@heroicons/react/24/outline'
import { useTranslation } from 'react-i18next'
import { useAppDispatch } from '../app/hooks'
import { uiActions } from '../app/store'

/** Introduces remote access and hands configuration off to Settings → Cloud. */
export function CloudMobileView() {
  const { t } = useTranslation()
  const dispatch = useAppDispatch()

  function openCloudSettings() {
    dispatch(uiActions.setSettingsTab('cloud'))
    dispatch(uiActions.setView('settings'))
  }

  const features = [
    { Icon: GlobeAltIcon, title: t('cloud.remoteAnywhereTitle'), desc: t('cloud.remoteAnywhereDesc') },
    { Icon: LockClosedIcon, title: t('cloud.remotePrivateTitle'), desc: t('cloud.remotePrivateDesc') },
    { Icon: ComputerDesktopIcon, title: t('cloud.remoteControlTitle'), desc: t('cloud.remoteControlDesc') },
  ]

  return (
    <div className="remote-access-page page-surface min-h-0 flex-1 overflow-y-auto">
      <section className="remote-access-hero">
        <div className="remote-access-copy">
          <div className="remote-access-eyebrow">
            <CloudIcon className="h-3.5 w-3.5" />
            {t('cloud.remoteEyebrow')}
          </div>
          <h1>{t('cloud.remoteTitle')}</h1>
          <p className="remote-access-lede">{t('cloud.remoteDesc')}</p>

          <button type="button" className="remote-access-cta group" onClick={openCloudSettings}>
            {t('cloud.remoteCta')}
            <ArrowRightIcon className="h-4 w-4 transition-transform group-hover:translate-x-0.5" />
          </button>
          <p className="remote-access-hint">
            <LockClosedIcon className="h-3.5 w-3.5" />
            {t('cloud.remoteCtaHint')}
          </p>
        </div>

        <div className="remote-access-visual" aria-hidden="true">
          <div className="remote-access-orbit remote-access-orbit-one" />
          <div className="remote-access-orbit remote-access-orbit-two" />

          <div className="remote-desktop">
            <div className="remote-window-bar">
              <span />
              <span />
              <span />
              <div className="remote-window-brand">
                <CloudIcon className="h-3.5 w-3.5" />
                jcode
              </div>
            </div>
            <div className="remote-desktop-body">
              <div className="remote-message remote-message-user">{t('cloud.remoteVisualPrompt')}</div>
              <div className="remote-message remote-message-agent">
                <span className="remote-agent-mark">J</span>
                <div>
                  <span />
                  <span />
                  <span />
                </div>
              </div>
              <div className="remote-run-status">
                <span className="remote-status-check"><CheckIcon className="h-3 w-3" /></span>
                {t('cloud.remoteVisualDone')}
              </div>
            </div>
          </div>

          <div className="remote-phone">
            <div className="remote-phone-speaker" />
            <div className="remote-phone-header">
              <span className="remote-status-dot" />
              {t('cloud.remoteVisualConnected')}
            </div>
            <div className="remote-phone-message" />
            <div className="remote-phone-message remote-phone-message-short" />
            <div className="remote-phone-action">
              <DevicePhoneMobileIcon className="h-3.5 w-3.5" />
              {t('cloud.remoteVisualApprove')}
            </div>
          </div>
        </div>
      </section>

      <section className="remote-access-features" aria-label={t('cloud.remoteFeaturesLabel')}>
        {features.map(({ Icon, title, desc }) => (
          <article key={title} className="remote-access-feature">
            <Icon className="h-5 w-5 shrink-0" />
            <div>
              <h2>{title}</h2>
              <p>{desc}</p>
            </div>
          </article>
        ))}
      </section>
    </div>
  )
}
