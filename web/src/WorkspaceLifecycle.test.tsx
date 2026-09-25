import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import App from './App'

const session = { authenticated: true, demoMode: false, user: { userId: 'owner', organizationId: 'org', displayName: 'Owner', email: 'owner@example.com', role: 'OWNER' } }
const workspaces = [{ id: 'org', name: 'Personal', role: 'OWNER' }, { id: 'team', name: 'Private Team', role: 'OWNER' }]
const invitation = { invitationId: 'invite', organizationId: 'team', organizationName: 'Private Team', role: 'MEMBER', expiresAt: '2030-01-01T00:00:00Z' }

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (error: Error) => void
  const promise = new Promise<T>((ok, fail) => { resolve = ok; reject = fail })
  return { promise, resolve, reject }
}

function setup() {
  const routes = new Map<string, () => Promise<Response>>()
  const clipboard = vi.fn().mockResolvedValue(undefined)
  vi.stubGlobal('navigator', { clipboard: { writeText: clipboard } })
  const fetchMock = vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
    const path = new URL(String(input), 'http://localhost').pathname
    const custom = routes.get(`${init?.method ?? 'GET'} ${path}`)
    if (custom) return custom()
    if (path.endsWith('/auth/session')) return Response.json(session)
    if (path.endsWith('/auth/logout') || path.endsWith('/me/account')) return new Response(null, { status: 204 })
    if (path.endsWith('/calendar/connection')) return Response.json({ connected: false })
    if (path.endsWith('/workspaces')) return Response.json({ workspaces })
    if (path.endsWith('/invitations/preview')) return Response.json(invitation)
    if (path.endsWith('/workspaces/switch')) return Response.json({ activeWorkspace: workspaces[1] })
    if (path.endsWith('/invitations/accept')) return Response.json({ accepted: true })
    if (path.endsWith('/invitations')) return Response.json({ inviteUrl: 'https://example.test/?invite=private-token' })
    if (path.endsWith('/projection')) return Response.json({ segments: [] })
    if (path.endsWith('/private-events')) return Response.json({ events: [] })
    return new Response('{}', { status: 404 })
  })
  window.history.replaceState({}, '', '/?auth=success&invite=incoming-token')
  const view = render(<App />)
  return { routes, clipboard, fetchMock, view }
}

async function openAccount() {
  fireEvent.click(await screen.findByRole('button', { name: 'Ownerのアカウントメニュー' }))
  await screen.findByRole('combobox', { name: 'Workspace' })
}

async function teardown(kind: 'logout' | 'delete') {
  if (kind === 'logout') {
    fireEvent.click(screen.getByRole('button', { name: 'ログアウト' }))
    await screen.findByText('ログアウトしました。')
  } else {
    fireEvent.click(screen.getByRole('button', { name: 'アカウントを削除' }))
    fireEvent.change(screen.getByLabelText('確認のため DELETE と入力'), { target: { value: 'DELETE' } })
    fireEvent.click(screen.getByRole('button', { name: '完全に削除する' }))
    await screen.findByText('アカウントと保存データを削除しました。')
  }
}

describe('Workspace mutations respect account lifetime', () => {
  afterEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals(); window.history.replaceState({}, '', '/') })

  it.each(['logout', 'delete'] as const)('does not copy a delayed invitation link after %s', async kind => {
    const h = setup()
    const body = deferred<object>()
    const response = Response.json({})
    response.json = () => body.promise
    h.routes.set('POST /api/v1/workspaces/org/invitations', async () => response)
    await openAccount()
    fireEvent.click(screen.getByRole('button', { name: '招待リンクを作成' }))
    await waitFor(() => expect(h.fetchMock.mock.calls.some(([url]) => String(url).endsWith('/org/invitations'))).toBe(true))
    await teardown(kind)
    await act(async () => { body.resolve({ inviteUrl: 'https://example.test/?invite=private-token' }) })
    expect(h.clipboard).not.toHaveBeenCalled()
    expect(screen.getByRole('status')).toHaveTextContent(kind === 'logout' ? 'ログアウトしました。' : 'アカウントと保存データを削除しました。')
  })

  it.each(['logout', 'delete'] as const)('does not send a follow-on workspace switch after %s', async kind => {
    const h = setup()
    const accepted = deferred<Response>()
    h.routes.set('POST /api/v1/invitations/accept', () => accepted.promise)
    await openAccount()
    fireEvent.click(await screen.findByRole('button', { name: '招待を受諾' }))
    await teardown(kind)
    await act(async () => { accepted.resolve(Response.json({ accepted: true })) })
    expect(h.fetchMock.mock.calls.some(([url]) => String(url).endsWith('/workspaces/switch'))).toBe(false)
    expect(screen.getByRole('status')).toHaveTextContent(kind === 'logout' ? 'ログアウトしました。' : 'アカウントと保存データを削除しました。')
  })

  it.each(['success', 'error'] as const)('does not overwrite logout with a late switch %s', async result => {
    const h = setup()
    const switched = deferred<Response>()
    h.routes.set('POST /api/v1/workspaces/switch', () => switched.promise)
    await openAccount()
    fireEvent.change(screen.getByRole('combobox', { name: 'Workspace' }), { target: { value: 'team' } })
    await teardown('logout')
    await act(async () => {
      if (result === 'success') switched.resolve(Response.json({ activeWorkspace: workspaces[1] }))
      else switched.reject(new Error('private error'))
    })
    expect(screen.getByRole('status')).toHaveTextContent('ログアウトしました。')
    expect(screen.queryByText(/Private Team/)).not.toBeInTheDocument()
  })

  it('does not copy a delayed link after unmount', async () => {
    const h = setup()
    const created = deferred<Response>()
    h.routes.set('POST /api/v1/workspaces/org/invitations', () => created.promise)
    await openAccount()
    fireEvent.click(screen.getByRole('button', { name: '招待リンクを作成' }))
    h.view.unmount()
    await act(async () => { created.resolve(Response.json({ inviteUrl: 'https://example.test/?invite=private-token' })) })
    expect(h.clipboard).not.toHaveBeenCalled()
  })

  it('creates and copies a current invitation, then clears its old-workspace link after switching', async () => {
    const h = setup()
    await openAccount()
    fireEvent.click(screen.getByRole('button', { name: '招待リンクを作成' }))
    expect(await screen.findByRole('textbox', { name: '招待リンク' })).toHaveValue('https://example.test/?invite=private-token')
    await waitFor(() => expect(h.clipboard).toHaveBeenCalledTimes(1))
    await waitFor(() => expect(screen.getByRole('combobox', { name: 'Workspace' })).toBeEnabled())
    fireEvent.change(screen.getByRole('combobox', { name: 'Workspace' }), { target: { value: 'team' } })
    expect(await screen.findByText('Private Team に切り替えました。')).toBeInTheDocument()
    expect(screen.queryByRole('textbox', { name: '招待リンク' })).not.toBeInTheDocument()
  })

  it('ignores clipboard completion after logout without undoing the already-issued copy', async () => {
    const h = setup()
    const copied = deferred<void>()
    h.clipboard.mockImplementation(() => copied.promise)
    await openAccount()
    fireEvent.click(screen.getByRole('button', { name: '招待リンクを作成' }))
    await waitFor(() => expect(h.clipboard).toHaveBeenCalledTimes(1))
    await teardown('logout')
    await act(async () => { copied.resolve() })
    expect(screen.getByRole('status')).toHaveTextContent('ログアウトしました。')
    expect(h.clipboard).toHaveBeenCalledTimes(1)
  })

  it('ignores invitation acceptance switch decoding after logout', async () => {
    const h = setup()
    const body = deferred<object>()
    const response = Response.json({})
    response.json = () => body.promise
    h.routes.set('POST /api/v1/workspaces/switch', async () => response)
    await openAccount()
    fireEvent.click(await screen.findByRole('button', { name: '招待を受諾' }))
    await waitFor(() => expect(h.fetchMock.mock.calls.some(([url]) => String(url).endsWith('/workspaces/switch'))).toBe(true))
    await teardown('logout')
    await act(async () => { body.resolve({ activeWorkspace: workspaces[1] }) })
    expect(screen.getByRole('status')).toHaveTextContent('ログアウトしました。')
    expect(screen.queryByText(/Private Team/)).not.toBeInTheDocument()
  })

  it('does not cancel an invitation when only Calendar is disconnected', async () => {
    const h = setup()
    h.routes.set('GET /api/v1/calendar/connection', async () => Response.json({ connected: true, connection: { connectedAt: '2026-09-01T00:00:00Z', grantedScopes: [], reconnectRequired: false } }))
    h.routes.set('DELETE /api/v1/calendar/connection', async () => new Response(null, { status: 204 }))
    const created = deferred<Response>()
    h.routes.set('POST /api/v1/workspaces/org/invitations', () => created.promise)
    await openAccount()
    fireEvent.click(screen.getByRole('button', { name: '招待リンクを作成' }))
    fireEvent.click(await screen.findByRole('button', { name: 'Calendar接続を解除' }))
    await screen.findByText('Google Calendarの接続と同期済みbusy時間を削除しました。')
    await act(async () => { created.resolve(Response.json({ inviteUrl: 'https://example.test/?invite=current-token' })) })
    expect(h.clipboard).toHaveBeenCalledWith('https://example.test/?invite=current-token')
    expect(screen.getByRole('textbox', { name: '招待リンク' })).toHaveValue('https://example.test/?invite=current-token')
  })

  it.each(['logout', 'delete'] as const)('preserves a current invitation when %s is rejected', async kind => {
    const h = setup()
    h.routes.set(kind === 'logout' ? 'POST /api/v1/auth/logout' : 'DELETE /api/v1/me/account', async () => new Response(null, { status: kind === 'logout' ? 503 : 409 }))
    const created = deferred<Response>()
    h.routes.set('POST /api/v1/workspaces/org/invitations', () => created.promise)
    await openAccount()
    fireEvent.click(screen.getByRole('button', { name: '招待リンクを作成' }))
    if (kind === 'logout') {
      fireEvent.click(screen.getByRole('button', { name: 'ログアウト' }))
      await screen.findByText('ログアウトできませんでした。')
    } else {
      fireEvent.click(screen.getByRole('button', { name: 'アカウントを削除' }))
      fireEvent.change(screen.getByLabelText('確認のため DELETE と入力'), { target: { value: 'DELETE' } })
      fireEvent.click(screen.getByRole('button', { name: '完全に削除する' }))
      await screen.findByText(/共有Workspaceの最後のOWNERです/)
    }
    await act(async () => { created.resolve(Response.json({ inviteUrl: 'https://example.test/?invite=current-token' })) })
    expect(h.clipboard).toHaveBeenCalledWith('https://example.test/?invite=current-token')
    expect(screen.getByRole('button', { name: 'Ownerのアカウントメニュー' })).toBeInTheDocument()
  })
})
