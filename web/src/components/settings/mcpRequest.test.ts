import { describe, expect, it } from 'vitest'
import { buildMCPRequest, emptyMCPForm, type MCPForm } from '../SettingsView'

function edited(overrides: Partial<MCPForm> = {}): MCPForm {
  return {
    ...emptyMCPForm(),
    name: 'docs',
    transport: 'http',
    url: 'https://mcp.example.test',
    headers: [
      { key: 'Authorization', value: 'sk-a...z' },
      { key: 'X-Trace', value: 'trace-mask' },
    ],
    originalHeaderKeys: ['Authorization', 'X-Trace'],
    oauthEnabled: true,
    oauthConfigured: true,
    clientId: 'client-id',
    clientSecretPresent: true,
    scopesText: 'openid profile',
    ...overrides,
  }
}

describe('MCP Settings secret mutation contract', () => {
  it('sends removed header names explicitly while retaining masked rows', () => {
    const form = edited({ headers: [{ key: 'X-Trace', value: 'trace-mask' }] })
    const request = buildMCPRequest(form, true)
    expect(request.remove_headers).toEqual(['Authorization'])
    expect(request.headers).toEqual({ 'X-Trace': 'trace-mask' })
  })

  it('treats clearing a saved header value as an explicit removal', () => {
    const request = buildMCPRequest(edited({
      headers: [
        { key: 'Authorization', value: '' },
        { key: 'X-Trace', value: 'trace-mask' },
      ],
    }), true)
    expect(request.remove_headers).toEqual(['Authorization'])
    expect(request.headers).toEqual({ 'X-Trace': 'trace-mask' })
  })

  it('uses remove_client_secret instead of treating an empty field as deletion', () => {
    const request = buildMCPRequest(edited({ removeClientSecret: true }), true)
    expect(request.oauth).toMatchObject({
      enabled: true,
      client_id: 'client-id',
      remove_client_secret: true,
    })
    expect(request.oauth?.client_secret).toBeUndefined()
  })

  it('removes the entire OAuth configuration only through remove_oauth', () => {
    const request = buildMCPRequest(edited({ oauthEnabled: false, removeOAuth: true }), true)
    expect(request.remove_oauth).toBe(true)
    expect(request.oauth).toBeUndefined()
  })

  it('cleans remote secrets when an edited server becomes local', () => {
    const request = buildMCPRequest(edited({ transport: 'local', command: 'mcp-server' }), true)
    expect(request.remove_headers).toEqual(['Authorization', 'X-Trace'])
    expect(request.remove_oauth).toBe(true)
  })

  it('keeps an existing secret when the password field remains blank', () => {
    const request = buildMCPRequest(edited(), true)
    expect(request.oauth?.client_secret).toBeUndefined()
    expect(request.oauth?.remove_client_secret).toBeUndefined()
  })
})
