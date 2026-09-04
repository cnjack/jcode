import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, within } from '@testing-library/react'
import { SelectMenu } from './SelectMenu'

const options = [
  { value: 'daily', label: 'Daily' },
  { value: 'cron', label: 'Cron' },
]

afterEach(cleanup)

describe('SelectMenu', () => {
  it('opens an app-themed listbox and selects an option', () => {
    const onChange = vi.fn()
    render(<SelectMenu ariaLabel="Trigger" value="daily" options={options} onChange={onChange} />)

    const trigger = screen.getByRole('button', { name: 'Trigger' })
    fireEvent.click(trigger)
    const listbox = screen.getByRole('listbox', { name: 'Trigger' })
    expect(within(listbox).getByRole('option', { name: 'Daily' }).getAttribute('aria-selected')).toBe('true')

    fireEvent.click(within(listbox).getByRole('option', { name: 'Cron' }))
    expect(onChange).toHaveBeenCalledWith('cron')
    expect(screen.queryByRole('listbox', { name: 'Trigger' })).toBeNull()
  })

  it('supports arrow, end, enter, and escape keyboard controls', () => {
    const onChange = vi.fn()
    render(<SelectMenu ariaLabel="Trigger" value="daily" options={options} onChange={onChange} />)

    const trigger = screen.getByRole('button', { name: 'Trigger' })
    fireEvent.keyDown(trigger, { key: 'ArrowDown' })
    let listbox = screen.getByRole('listbox', { name: 'Trigger' })
    fireEvent.keyDown(listbox, { key: 'End' })
    fireEvent.keyDown(listbox, { key: 'Enter' })
    expect(onChange).toHaveBeenCalledWith('cron')

    fireEvent.click(trigger)
    listbox = screen.getByRole('listbox', { name: 'Trigger' })
    fireEvent.keyDown(listbox, { key: 'Escape' })
    expect(screen.queryByRole('listbox', { name: 'Trigger' })).toBeNull()
  })
})
