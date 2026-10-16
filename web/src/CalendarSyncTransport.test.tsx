import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import App from './App'
import { readCalendarSync } from './calendarSyncResponse'

function deferred<T>() { let resolve!: (v: T) => void; const promise = new Promise<T>(done => { resolve = done }); return { promise, resolve } }
const successful = (count = 3) => ({ synced: true, busySpanCount: count, lastSyncedAt: new Date().toISOString() })
function setup() {
  const routes = new Map<string, () => Promise<Response>>()
  const fetch = vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
    const path = new URL(String(input), 'http://localhost').pathname
    if (routes.has(path)) return routes.get(path)!()
    if (path.endsWith('/auth/session')) return Response.json({ authenticated: true, user: { userId: 'owner', organizationId: 'org', displayName: 'Owner', email: '', role: 'OWNER' } })
    if (path.endsWith('/calendar/connection')) return init?.method === 'DELETE' ? new Response(null, { status: 204 }) : Response.json({ connected: true, connection: { connectedAt: '2026-01-01T00:00:00Z', grantedScopes: [], reconnectRequired: false } })
    if (path.endsWith('/workspaces')) return Response.json({ workspaces: [] })
    if (path.endsWith('/auth/logout')) return new Response(null, { status: 204 })
    if (path.endsWith('/private-events')) return Response.json({ events: [] })
    if (path.endsWith('/projection')) return Response.json({ segments: [] })
    return new Response(null, { status: 404 })
  })
  window.history.replaceState({}, '', '/?auth=success')
  const view = render(<App />)
  return { routes, fetch, view }
}
async function openAccount() { fireEvent.click(await screen.findByRole('button', { name: 'Ownerのアカウントメニュー' })); await screen.findByRole('button', { name: 'busy時間を同期' }); await act(async () => {}) }
afterEach(() => { cleanup(); vi.useRealTimers(); vi.restoreAllMocks(); window.history.replaceState({}, '', '/') })

it.each(['fetch', 'body'])('bounds stalled sync %s without auto-retrying and ignores late outcomes', async phase => {
  const h = setup(), late = deferred<Response>(), body = deferred<object>(), response = Response.json({}); response.json = () => body.promise
  h.routes.set('/api/v1/calendar/sync', () => phase === 'fetch' ? late.promise : Promise.resolve(response))
  await openAccount()
  vi.useFakeTimers()
  await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'busy時間を同期' })) })
  await act(async () => { await vi.advanceTimersByTimeAsync(20_000) })
  expect(screen.getByText(/Calendarの同期結果を確認できませんでした/)).toBeInTheDocument()
  expect(screen.getByRole('button', { name: 'busy時間を同期' })).toBeEnabled()
  const calls = h.fetch.mock.calls.filter(([url]) => String(url).endsWith('/calendar/sync'))
  expect(calls).toHaveLength(1)
  expect(calls[0][1]?.signal?.aborted).toBe(true)
  h.routes.set('/api/v1/calendar/sync', async () => Response.json(successful(4)))
  await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'busy時間を同期' })) })
  await act(async () => { late.resolve(Response.json(successful(99))); body.resolve(successful(99)) })
  expect(screen.getByText(/Google Calendarから4件のbusy時間/)).toBeInTheDocument()
  expect(screen.queryByText(/99件/)).not.toBeInTheDocument()
  h.view.unmount()
  expect(vi.getTimerCount()).toBe(0)
})

it.each([{}, { synced: false, busySpanCount: 3, lastSyncedAt: '2026-01-01T00:00:00Z' }, { synced: true, busySpanCount: -1, lastSyncedAt: '2026-01-01T00:00:00Z' }, { synced: true, busySpanCount: 3, lastSyncedAt: 'not-a-date' }])('does not claim successful sync from malformed 2xx', async payload => {
  const h = setup(); h.routes.set('/api/v1/calendar/sync', async () => Response.json(payload))
  await openAccount(); fireEvent.click(screen.getByRole('button', { name: 'busy時間を同期' }))
  await screen.findByText(/Calendarの同期結果を確認できませんでした/)
  expect(screen.queryByText(/busy時間を同期しました/)).not.toBeInTheDocument()
  expect(screen.getByRole('button', { name: 'busy時間を同期' })).toBeEnabled()
})

it('bounds the post-sync display refresh and preserves the acknowledged sync result', async () => {
  const h = setup(), body = deferred<object>(), response = Response.json({}); response.json = () => body.promise
  h.routes.set('/api/v1/calendar/sync', async () => Response.json(successful()))
  await openAccount()
  h.routes.set('/api/v1/people/owner/projection', async () => response)
  vi.useFakeTimers()
  await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'busy時間を同期' })) })
  await act(async () => { await vi.advanceTimersByTimeAsync(20_000) })
  expect(screen.getByText(/3件を同期しましたが、表示の再読込に失敗/)).toBeInTheDocument()
  expect(screen.getByRole('button', { name: 'busy時間を同期' })).toBeEnabled()
  // Logout also fences the independent private-calendar refresh effect.
  await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'ログアウト' })) })
  await act(async () => { body.resolve({ segments: [] }) })
  expect(screen.getByRole('status')).toHaveTextContent('ログアウトしました。')
})

it('ignores a pending sync body after unmount', async () => {
  const h = setup(), body = deferred<object>(), response = Response.json({}); response.json = () => body.promise
  h.routes.set('/api/v1/calendar/sync', async () => response)
  await openAccount(); fireEvent.click(screen.getByRole('button', { name: 'busy時間を同期' }))
  await waitFor(() => expect(h.fetch.mock.calls.some(([url]) => String(url).endsWith('/calendar/sync'))).toBe(true))
  const before = h.fetch.mock.calls.length
  h.view.unmount()
  await act(async () => { body.resolve(successful()) })
  expect(h.fetch.mock.calls).toHaveLength(before)
})

it('rejects unsafe counts and future timestamps', async () => {
  await expect(readCalendarSync(Response.json({ ...successful(), busySpanCount: 1.2 }))).rejects.toThrow()
  await expect(readCalendarSync(Response.json({ ...successful(), busySpanCount: Number.MAX_SAFE_INTEGER + 1 }))).rejects.toThrow()
  await expect(readCalendarSync(Response.json({ ...successful(), lastSyncedAt: new Date(Date.now() + 86400000).toISOString() }))).rejects.toThrow()
  await expect(readCalendarSync(Response.json(successful(0)))).resolves.toMatchObject({ busySpanCount: 0 })
})
