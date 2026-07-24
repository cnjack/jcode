import { beforeEach, describe, expect, it } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { Provider } from 'react-redux'
import { i18n } from '../i18n'
import { store, uiActions } from '../app/store'
import { CloudMobileView } from './CloudMobileView'

function renderView() {
  return render(
    <Provider store={store}>
      <CloudMobileView />
    </Provider>,
  )
}

beforeEach(async () => {
  cleanup()
  await i18n.changeLanguage('en')
  store.dispatch(uiActions.setView('cloud-mobile'))
  store.dispatch(uiActions.setSettingsTab('general'))
})

describe('CloudMobileView', () => {
  it('introduces remote access without embedding cloud settings', () => {
    renderView()
    expect(screen.getByRole('heading', { level: 1, name: 'Step away. Keep the work moving.' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Set up remote access' })).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Log in to cloud' })).toBeNull()
  })

  it('deep-links the call to action to Settings → Cloud', () => {
    renderView()
    fireEvent.click(screen.getByRole('button', { name: 'Set up remote access' }))
    expect(store.getState().ui.activeView).toBe('settings')
    expect(store.getState().ui.settingsTab).toBe('cloud')
  })
})
