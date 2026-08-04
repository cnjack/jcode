import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { Provider } from 'react-redux'
import type { ThreadItem } from 'jcode-ui-core'
import { chatActions, store } from '../app/store'
import { i18n } from '../i18n'
import { ComputerShotPiP } from './ComputerShotPiP'

beforeEach(async () => {
  cleanup()
  await i18n.changeLanguage('en')
  store.dispatch(chatActions.clearChat())
})

afterEach(() => {
  cleanup()
  store.dispatch(chatActions.clearChat())
})

function renderPip(timeline: ThreadItem[]) {
  store.dispatch(chatActions.setTimeline(timeline))
  return render(<Provider store={store}><ComputerShotPiP /></Provider>)
}

describe('ComputerShotPiP', () => {
  it('shows computer-use screenshots and ignores browser screenshots', () => {
    renderPip([
      {
        kind: 'tool',
        seq: 1,
        data: {
          id: 'computer-1',
          name: 'computer_screenshot',
          args: '{}',
          status: 'done',
          timestamp: 1,
          output: 'image_ref=/api/computer/shots/older.png',
        },
      },
      {
        kind: 'tool',
        seq: 2,
        data: {
          id: 'browser-1',
          name: 'browser_screenshot',
          args: '{}',
          status: 'done',
          timestamp: 2,
          output: 'image_ref=/api/browser/shots/latest.png',
        },
      },
    ])

    expect(screen.getByText('Computer use')).toBeTruthy()
    expect(screen.queryByText('Browser')).toBeNull()
    expect(screen.getByRole('img', { name: 'Computer use' }).getAttribute('src')).toContain('/api/computer/shots/older.png')

    fireEvent.click(screen.getByTitle('Minimize'))
    expect(screen.getByText('Computer use')).toBeTruthy()
    expect(screen.getByTitle('Show')).toBeTruthy()
  })
})
