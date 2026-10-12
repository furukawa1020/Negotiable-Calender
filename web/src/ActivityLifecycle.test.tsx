import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import App from './App'

function deferred<T>() { let resolve!: (value: T) => void; const promise = new Promise<T>(done => { resolve = done }); return { promise, resolve } }
const notice = { id: 'n', message: 'Private notification', type: 'request_received', requestId: 'r', createdAt: '2030-01-01T00:00:00Z' }
const audit = { id: 'a', organizationId: 'org', actorUserId: 'Private actor', action: 'request.accepted', resourceType: 'request', resourceId: 'r', createdAt: '2030-01-01T00:00:00Z' }
function setup() {
  const routes = new Map<string, () => Promise<Response>>()
  const fetch = vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
    const path = new URL(String(input), 'http://localhost').pathname
    const custom = routes.get(`${init?.method ?? 'GET'} ${path}`)
    if (custom) return custom()
    if (path.endsWith('/auth/session')) return Response.json({ authenticated: true, demoMode: false, user: { userId: 'owner', organizationId: 'org', displayName: 'Owner', email: 'owner@example.com', role: 'OWNER' } })
    if (path.endsWith('/calendar/connection')) return Response.json({ connected: false })
    if (path.endsWith('/auth/logout') || path.endsWith('/me/account')) return new Response(null, { status: 204 })
    if (path.endsWith('/workspaces')) return Response.json({ workspaces: [{ id: 'org', name: 'Personal', role: 'OWNER' }, { id: 'team', name: 'Team', role: 'OWNER' }] })
    if (path.endsWith('/workspaces/switch')) return Response.json({ activeWorkspace: { id: 'team', name: 'Team', role: 'OWNER' } })
    return new Response('{}', { status: 404 })
  })
  window.history.replaceState({}, '', '/?auth=success')
  const view = render(<App />)
  return { routes, fetch, view }
}
afterEach(() => { cleanup(); vi.useRealTimers(); vi.restoreAllMocks(); window.history.replaceState({}, '', '/') })

it.each(['通知', '監査ログ'])('ignores late %s bodies after logout or workspace switch', async label => {
  for (const boundary of ['logout', 'switch']) {
    const h = setup(), body = deferred<object>()
    const response = Response.json({}); response.json = () => body.promise
    h.routes.set(`GET /api/v1/${label === '通知' ? 'notifications' : 'audit-logs'}`, async () => response)
    await screen.findByRole('button', { name: 'Ownerのアカウントメニュー' })
    fireEvent.click(screen.getByRole('button', { name: label === '通知' ? label : '監査' }))
    await waitFor(() => expect(h.fetch.mock.calls.some(([url]) => String(url).includes(label === '通知' ? '/notifications' : '/audit-logs'))).toBe(true))
    fireEvent.click(screen.getByRole('button', { name: 'Ownerのアカウントメニュー' }))
    if (boundary === 'logout') fireEvent.click(screen.getByRole('button', { name: 'ログアウト' }))
    else fireEvent.change(await screen.findByRole('combobox', { name: 'Workspace' }), { target: { value: 'team' } })
    await screen.findByText(boundary === 'logout' ? 'ログアウトしました。' : 'Team に切り替えました。')
    await act(async () => { body.resolve(label === '通知' ? { notifications: [notice] } : { auditLogs: [audit] }) })
    expect(screen.queryByText(/Private/)).not.toBeInTheDocument()
    h.view.unmount(); vi.restoreAllMocks()
  }
})

it('does not let an older notification snapshot undo a read acknowledgement', async () => {
  const h = setup(), old = deferred<Response>()
  h.routes.set('GET /api/v1/notifications', async () => Response.json({ notifications: [notice] }))
  h.routes.set('POST /api/v1/notifications/n/read', async () => new Response(null, { status: 204 }))
  await screen.findByRole('button', { name: 'Ownerのアカウントメニュー' })
  fireEvent.click(screen.getByRole('button', { name: '通知' }))
  await screen.findByText('Private notification')
  fireEvent.click(screen.getByRole('button', { name: '通知' }))
  h.routes.set('GET /api/v1/notifications', () => old.promise)
  fireEvent.click(screen.getByRole('button', { name: '通知' }))
  fireEvent.click(screen.getByText('Private notification'))
  await waitFor(() => expect(screen.getByText('Private notification').closest('button')).toHaveClass('read'))
  await act(async () => { old.resolve(Response.json({ notifications: [notice] })) })
  expect(screen.getByText('Private notification').closest('button')).toHaveClass('read')
})

it.each(['通知', '監査ログ'])('bounds stalled %s decoding and ignores late results after retry', async label => {
  const h = setup(), body = deferred<object>()
  const response = Response.json({}); response.json = () => body.promise
  const path = `/api/v1/${label === '通知' ? 'notifications' : 'audit-logs'}`
  h.routes.set(`GET ${path}`, async () => response)
  await screen.findByRole('button', { name: 'Ownerのアカウントメニュー' })
  vi.useFakeTimers()
  await act(async () => { fireEvent.click(screen.getByRole('button', { name: label === '通知' ? label : '監査' })) })
  const signal = h.fetch.mock.calls.find(([url]) => String(url).endsWith(path))?.[1]?.signal
  await act(async () => { await vi.advanceTimersByTimeAsync(20_000) })
  expect(signal?.aborted).toBe(true)
  expect(screen.getByText(`${label}を取得できませんでした。`)).toBeInTheDocument()
  h.routes.set(`GET ${path}`, async () => Response.json(label === '通知' ? { notifications: [] } : { auditLogs: [] }))
  await act(async () => {
    if (label === '通知') fireEvent.click(screen.getByRole('button', { name: label }))
    fireEvent.click(screen.getByRole('button', { name: label === '通知' ? label : '監査' }))
  })
  await act(async () => { body.resolve(label === '通知' ? { notifications: [notice] } : { auditLogs: [audit] }) })
  expect(screen.queryByText(/Private/)).not.toBeInTheDocument()
  expect(vi.getTimerCount()).toBe(0)
})
