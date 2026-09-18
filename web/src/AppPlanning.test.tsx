import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import App from './App'

afterEach(() => { cleanup(); vi.restoreAllMocks(); vi.unstubAllGlobals(); window.history.replaceState({}, '', '/') })

it.each([['owner', 'suggested', true], ['other', 'suggested', false], ['owner', 'accepted', false]])('scopes local planning to the signed-in target (%s/%s)', async (targetUserId, status, visible) => {
  window.history.replaceState({}, '', '/?auth=success')
  vi.stubGlobal('LanguageModel', undefined)
  const at = (milliseconds: number) => new Date(Date.now() + milliseconds).toISOString()
  const request = { id: 'request-1', requesterUserId: 'requester', targetUserId, title: 'Planning integration', durationMinutes: 30, deadlineAt: at(86400000), priority: 'normal', status, options: [{ id: 'option-1', type: 'meeting', startAt: at(3600000), endAt: at(5400000) }] }
  const preview = { revision: 'a'.repeat(64), expiresAt: at(60000), candidates: [{ id: 'c1', startAt: request.options[0].startAt, endAt: request.options[0].endAt }] }
  const fetch = vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
    const url = String(input)
    let value: object = {}
    if (url.includes('/auth/session')) value = { authenticated: true, demoMode: false, user: { userId: 'owner', organizationId: 'workspace', email: 'owner@example.com', displayName: 'Owner', role: 'OWNER' } }
    else if (url.includes('/calendar/connection')) value = { connected: false }
    else if (url.includes('/workspaces')) value = { activeWorkspaceId: 'workspace', workspaces: [{ id: 'workspace', name: 'Workspace', role: 'OWNER' }] }
    else if (url.endsWith('/planning-preview')) value = preview
    else if (url.endsWith('/requests')) value = { requests: [request] }
    return new Response(JSON.stringify(value), { status: 200 })
  })
  render(<App />)
  await screen.findByRole('button', { name: 'Ownerのアカウントメニュー' })
  fireEvent.click(screen.getByRole('button', { name: '依頼' }))
  await screen.findByRole('heading', { name: 'Planning integration' })
  const button = screen.queryByRole('button', { name: '端末内 AI で候補を比べる' })
  expect(Boolean(button)).toBe(visible)
  expect(fetch.mock.calls.some(([url]) => String(url).endsWith('/planning-preview'))).toBe(false)
  if (!button) return
  fireEvent.click(button)
  await screen.findByText(/この環境では端末内 AI/)
  expect(fetch).toHaveBeenCalledWith(expect.stringContaining('/requests/request-1/planning-preview'), expect.objectContaining({ credentials: 'include', signal: expect.any(AbortSignal), headers: { 'X-Demo-User-ID': 'owner', 'X-Organization-ID': 'workspace' } }))
  // Parent source change re-keys the panel and drops consent/output immediately.
  request.options = [{ ...request.options[0], id: 'changed-option' }]
  fireEvent.click(screen.getByRole('button', { name: '更新' }))
  await waitFor(() => expect(screen.getByRole('button', { name: '端末内 AI で候補を比べる' })).toBeInTheDocument())
  expect(screen.queryByText(/この環境では端末内 AI/)).not.toBeInTheDocument()
})
