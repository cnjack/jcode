import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { GeneratedImageCard } from './GeneratedImageCard.js'

afterEach(cleanup)

describe('GeneratedImageCard', () => {
  it('renders queued as a neutral pre-dispatch state with stable geometry', () => {
    const { container } = render(
      <GeneratedImageCard state="queued" model="CogView-3-Flash" aspectRatio="16:9" />,
    )
    expect(screen.getByRole('status').textContent).toBe('Preparing image request')
    expect(screen.getByText('No provider request has been sent.')).toBeTruthy()
    expect(container.querySelector('section')?.getAttribute('style')).toContain('1.777')
  })

  it('keeps generating and saving previews compact at the requested ratio', () => {
    const view = render(
      <GeneratedImageCard
        state="generating"
        provider="alibaba-token-plan-cn"
        model="wan2.7-image"
        aspectRatio="9:16"
        startedAt={Date.now() - 41_000}
      />,
    )
    const region = screen.getByRole('region', { name: 'Generating' })
    expect(region.style.aspectRatio).toBe('0.5625')
    expect(region.style.getPropertyValue('--jcode-generated-image-preview-width')).toBe('12rem')
    expect(region.querySelectorAll('.jcode-generated-image__weave-rail.is-horizontal')).toHaveLength(8)
    expect(region.querySelectorAll('.jcode-generated-image__weave-rail.is-vertical')).toHaveLength(8)
    expect(region.querySelectorAll('.jcode-generated-image__weave-node')).toHaveLength(6)
    expect(region.querySelectorAll('.jcode-generated-image__weave-node.is-settling')).toHaveLength(0)
    expect(region.querySelector('.jcode-generated-image__pixel-shutter')).toBeNull()
    expect(region.querySelector('.jcode-generated-image__status')).toBeNull()
    const liveStatus = screen.getByRole('status')
    expect(liveStatus.classList.contains('jcode-generated-image__sr-status')).toBe(true)
    expect(liveStatus.getAttribute('aria-atomic')).toBe('true')
    expect(region.contains(liveStatus)).toBe(false)
    expect(liveStatus.parentElement).toBe(region.parentElement)
    expect(liveStatus.previousElementSibling).toBe(region)
    expect(screen.queryByText('alibaba-token-plan-cn · wan2.7-image')).toBeNull()
    expect(screen.queryByText('41s')).toBeNull()

    view.rerender(<GeneratedImageCard state="saving" aspectRatio="16:9" />)
    expect(region.getAttribute('data-generated-image-state')).toBe('saving')
    expect(region.style.getPropertyValue('--jcode-generated-image-preview-width')).toBe('18rem')
    expect(region.querySelectorAll('.jcode-generated-image__weave-node.is-settling')).toHaveLength(6)
    expect(screen.getByRole('status').textContent).toBe('Saving')
  })

  it('opens a decoded image accessibly without intercepting its other actions', async () => {
    const openImage = vi.fn()
    const download = vi.fn()
    const open = vi.fn()
    const reveal = vi.fn()
    render(
      <GeneratedImageCard
        state="succeeded"
        imageSrc="blob:image"
        artifact={{
          id: 'a1',
          storage: 'managed',
          key: 'images/a1.png',
          title: 'Generated image',
          kind: 'image',
          media_type: 'image/png',
          size: 2048,
          width: 1024,
          height: 1024,
        }}
        onOpenImage={openImage}
        onDownload={download}
        onOpenArtifact={open}
        onReveal={reveal}
      />,
    )
    expect(screen.queryByRole('button', { name: 'Open image in a new window' })).toBeNull()
    fireEvent.load(screen.getByRole('img', { name: 'Image generated' }))
    const preview = await screen.findByRole('button', { name: 'Open image in a new window' })
    expect(preview.getAttribute('type')).toBe('button')
    fireEvent.click(preview)
    fireEvent.click(screen.getByRole('button', { name: 'Download image' }))
    fireEvent.click(screen.getByRole('button', { name: 'Open Artifact' }))
    fireEvent.click(screen.getByRole('button', { name: 'Reveal in folder' }))
    expect(download).toHaveBeenCalledOnce()
    expect(openImage).toHaveBeenCalledOnce()
    expect(open).toHaveBeenCalledOnce()
    expect(reveal).toHaveBeenCalledOnce()
    expect(screen.queryByRole('button', { name: /edit/i })).toBeNull()
    expect(screen.queryByRole('button', { name: /regenerate/i })).toBeNull()
    expect(screen.queryByRole('button', { name: /retry/i })).toBeNull()
  })

  it('reveals a successful image without the compact busy-width override', async () => {
    render(
      <GeneratedImageCard
        state="succeeded"
        imageSrc="blob:image"
        aspectRatio="3:2"
        alt="A generated landscape"
      />,
    )
    const region = screen.getByRole('region', { name: 'Image generated' })
    fireEvent.load(screen.getByRole('img', { name: 'A generated landscape' }))
    await waitFor(() => expect(region.classList.contains('is-image-ready')).toBe(true))
    expect(region.style.aspectRatio).toBe('1.5')
    expect(region.style.getPropertyValue('--jcode-generated-image-width')).toBe('18rem')
    expect(region.style.getPropertyValue('--jcode-generated-image-preview-width')).toBe('')
  })

  it('uses typed auth errors for a settings deep link without rendering raw args', () => {
    const openSettings = vi.fn()
    const { container } = render(
      <GeneratedImageCard
        state="failed"
        errorCode="invalid_api_key"
        errorMessage="Generic failure must not win"
        strings={{ authError: 'Credential snapshot rejected' }}
        onOpenSettings={openSettings}
      />,
    )
    expect(screen.getByText('Credential snapshot rejected')).toBeTruthy()
    expect(screen.queryByText('Generic failure must not win')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: 'Open provider settings' }))
    expect(openSettings).toHaveBeenCalledOnce()
    expect(container.textContent).not.toContain('prompt')
  })

  it('maps quota, safety, rate, download, and persist codes before a generic message', () => {
    const cases = [
      ['quota_exceeded', 'Quota typed'],
      ['safety_blocked', 'Safety typed'],
      ['rate_limited', 'Rate typed'],
      ['asset_download_failed', 'Download typed'],
      ['artifact_persist_failed', 'Persist typed'],
    ] as const
    for (const [errorCode, expected] of cases) {
      const view = render(
        <GeneratedImageCard
          state="failed"
          errorCode={errorCode}
          errorMessage="Generic failure must not win"
          strings={{
            quotaError: 'Quota typed',
            safetyError: 'Safety typed',
            rateLimitError: 'Rate typed',
            downloadError: 'Download typed',
            persistError: 'Persist typed',
          }}
        />,
      )
      expect(screen.getByText(expected)).toBeTruthy()
      expect(screen.queryByText('Generic failure must not win')).toBeNull()
      view.unmount()
    }
  })

  it('labels uncertain operations as potentially billable and exposes no retry', () => {
    render(<GeneratedImageCard state="uncertain" model="CogView-3-Flash" />)
    expect(screen.getByRole('status').textContent).toBe('Status unknown')
    expect(screen.getByText(/may have accepted the request/i)).toBeTruthy()
    expect(screen.queryByRole('button', { name: /retry/i })).toBeNull()
  })

  it('leaves the busy state when a persisted image cannot be decoded', () => {
    render(<GeneratedImageCard state="succeeded" imageSrc="blob:broken" />)
    fireEvent.error(screen.getByRole('img'))
    expect(screen.getByRole('status').textContent).toBe('Image generated')
    expect(screen.getByText('The saved image could not be loaded.')).toBeTruthy()
    expect(screen.getByRole('region', { name: 'Image generated' }).getAttribute('aria-busy')).toBe('false')
  })
})
