import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { ReactNode } from 'react'
import type { Message as MessageData } from 'jcode-ui-core'
import { RuntimeProvider, createMockRuntime } from 'jcode-ui-core/runtime'
import { Message } from './Message.js'

afterEach(cleanup)

function message(role: MessageData['role'], id: string): MessageData {
  return {
    id,
    role,
    content: role === 'user' ? 'Please run the checks' : 'All checks passed.',
    timestamp: 1_000,
  }
}

function renderMessages(children: ReactNode) {
  return render(
    <RuntimeProvider runtime={createMockRuntime()}>
      {children}
    </RuntimeProvider>,
  )
}

describe('Message split conversation layout', () => {
  it('renders the user as a right-lane bubble and the assistant as flat prose without default avatars', () => {
    const { container } = renderMessages(
      <>
        <Message message={message('user', 'user')} />
        <Message message={message('assistant', 'assistant')} />
      </>,
    )

    const user = screen.getByRole('article', { name: 'You' })
    const assistant = screen.getByRole('article', { name: 'JCODE' })
    expect(user.getAttribute('data-role')).toBe('user')
    expect(user.querySelector('.jcode-message-bubble')).toBeTruthy()
    expect(user.querySelector('.jcode-message__footer')).toBeTruthy()
    expect(assistant.querySelector('.jcode-message-bubble')).toBeNull()
    expect(assistant.querySelector('.jcode-message__body')).toBeTruthy()
    expect(container.querySelector('.jcode-msg-avatar')).toBeNull()
    expect(screen.queryByText('You')).toBeNull()
    expect(screen.queryByText('JCODE')).toBeNull()
  })

  it('keeps system identity visible and preserves opt-in custom avatar chrome', () => {
    renderMessages(
      <>
        <Message message={{ ...message('system', 'system'), level: 'error', content: 'Failed' }} />
        <Message
          message={message('user', 'custom-user')}
          slots={{ avatar: () => <span data-testid="custom-avatar">Custom avatar</span> }}
        />
      </>,
    )

    expect(screen.getByText('Error')).toBeTruthy()
    expect(screen.getByTestId('custom-avatar')).toBeTruthy()
    expect(within(screen.getByRole('article', { name: 'You' })).getByText('You')).toBeTruthy()
  })

  it('lets a custom header own the visible and accessible identity', () => {
    renderMessages(
      <Message
        message={message('assistant', 'custom-assistant')}
        slots={{ header: () => <h3>Claude reviewer</h3> }}
      />,
    )

    const article = screen.getByRole('article')
    expect(article.getAttribute('aria-label')).toBeNull()
    expect(within(article).getByRole('heading', { name: 'Claude reviewer' })).toBeTruthy()
  })

  it('keeps the editable user surface in the right-lane editor wrapper', () => {
    const { container } = renderMessages(<Message message={message('user', 'editable')} canEdit />)
    fireEvent.click(screen.getByRole('button', { name: 'Edit' }))

    expect(container.querySelector('.jcode-message[data-role="user"] .jcode-message__editor')).toBeTruthy()
    expect((screen.getByRole('textbox') as HTMLTextAreaElement).value).toBe('Please run the checks')
  })

  it('routes file links through the host save flow and leaves regular links alone', () => {
    const onDownloadFile = vi.fn(() => true)
    renderMessages(
      <Message
        message={{ ...message('assistant', 'download'), content: '[下载示例销售数据.xlsx](/api/tasks/task-1/artifacts/file-1/download)' }}
        onDownloadFile={onDownloadFile}
      />,
    )

    const fileLink = screen.getByRole('link', { name: '下载示例销售数据.xlsx' })
    expect(fireEvent.click(fileLink)).toBe(false)
    expect(onDownloadFile).toHaveBeenCalledWith(
      '/api/tasks/task-1/artifacts/file-1/download',
      '下载示例销售数据.xlsx',
    )

    renderMessages(
      <Message
        message={{ ...message('assistant', 'docs'), content: '[Documentation](https://example.com/docs)' }}
        onDownloadFile={onDownloadFile}
      />,
    )
    const documentationLink = screen.getByRole('link', { name: 'Documentation' })
    const preserveDefault = vi.fn((event: Event) => event.preventDefault())
    documentationLink.addEventListener('click', preserveDefault)
    expect(fireEvent.click(documentationLink)).toBe(false)
    expect(preserveDefault).toHaveBeenCalledOnce()
    expect(onDownloadFile).toHaveBeenCalledOnce()
  })

  it('keeps a service file link disabled until the host confirms it exists', async () => {
    const onDownloadFile = vi.fn(() => true)
    const validateDownloadFile = vi.fn().mockResolvedValue(false)
    const { container } = renderMessages(
      <Message
        message={{ ...message('assistant', 'missing-download'), content: '[sales.xlsx](/api/tasks/task-1/artifacts/missing/download)' }}
        onDownloadFile={onDownloadFile}
        validateDownloadFile={validateDownloadFile}
      />,
    )
    const link = container.querySelector('a')
    expect(link).toBeTruthy()

    await waitFor(() => expect(link?.getAttribute('aria-disabled')).toBe('true'))
    expect(link?.hasAttribute('href')).toBe(false)
    fireEvent.click(link!)
    expect(onDownloadFile).not.toHaveBeenCalled()
  })

  it('enables a service file link after the host confirms it exists', async () => {
    const onDownloadFile = vi.fn(() => true)
    const validateDownloadFile = vi.fn().mockResolvedValue(true)
    const { container } = renderMessages(
      <Message
        message={{ ...message('assistant', 'verified-download'), content: '[sales.xlsx](/api/tasks/task-1/artifacts/existing/download)' }}
        onDownloadFile={onDownloadFile}
        validateDownloadFile={validateDownloadFile}
      />,
    )
    const link = container.querySelector('a')
    expect(link).toBeTruthy()

    await waitFor(() => expect(link?.getAttribute('href')).toBe('/api/tasks/task-1/artifacts/existing/download'))
    expect(link?.getAttribute('aria-disabled')).toBeNull()
    expect(fireEvent.click(link!)).toBe(false)
    expect(onDownloadFile).toHaveBeenCalledWith(
      '/api/tasks/task-1/artifacts/existing/download',
      'sales.xlsx',
    )
  })

  it('enables a relative workspace file link after verification', async () => {
    const onDownloadFile = vi.fn(() => true)
    const validateDownloadFile = vi.fn().mockResolvedValue(true)
    const { container } = renderMessages(
      <Message
        message={{ ...message('assistant', 'workspace-download'), content: '[下载示例销售数据.xlsx](示例销售数据.xlsx)' }}
        onDownloadFile={onDownloadFile}
        validateDownloadFile={validateDownloadFile}
      />,
    )
    const link = container.querySelector('a')
    expect(link).toBeTruthy()
    const href = encodeURI('示例销售数据.xlsx')

    await waitFor(() => expect(link?.getAttribute('href')).toBe(href))
    expect(fireEvent.click(link!)).toBe(false)
    expect(validateDownloadFile).toHaveBeenCalledWith(href, '下载示例销售数据.xlsx')
    expect(onDownloadFile).toHaveBeenCalledWith(href, '下载示例销售数据.xlsx')
  })
})
