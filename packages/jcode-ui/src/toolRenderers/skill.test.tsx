import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import { SkillRenderer } from './skill.js'

afterEach(cleanup)

describe('SkillRenderer', () => {
  it('decodes escaped multiline descriptions and keeps the card compact', () => {
    const description = 'Use the spreadsheet skill.\nIt supports "quoted" text and \\ paths.'
    const output = `<skill name="xlsx" description=${JSON.stringify(description)}>\nInstructions\n</skill>`
    const { container } = render(<SkillRenderer name="load_skill" args='{"name":"xlsx"}' output={output} status="done" />)

    expect(screen.getByText('xlsx')).toBeTruthy()
    const summary = container.querySelector<HTMLElement>('.jcode-skill [title]')
    expect(summary?.getAttribute('title')).toBe(description)
    expect(summary?.textContent).toBe(description)
    expect(summary?.className).toContain('line-clamp-2')
  })
})
