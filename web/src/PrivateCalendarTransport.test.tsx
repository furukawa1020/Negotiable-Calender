import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import App from './App'

function deferred<T>() { let resolve!: (v: T) => void; const promise = new Promise<T>(done => { resolve = done }); return { promise, resolve } }
const event = { id: 'e', title: '前の期間の私用予定', allDay: true, startDate: '2026-10-17', endDate: '2026-10-18' }
function setup() {
  const routes = new Map<string, () => Promise<Response>>()
  const fetch = vi.spyOn(globalThis, 'fetch').mockImplementation(async input => {
    const path = new URL(String(input), 'http://localhost').pathname
    if (routes.has(path)) return routes.get(path)!()
    if (path.endsWith('/auth/session')) return Response.json({ authenticated: true, user: { userId: 'owner', organizationId: 'org', displayName: 'Owner', email: '', role: 'OWNER' } })
    if (path.endsWith('/calendar/connection')) return Response.json({ connected: true, connection: { connectedAt: '2026-01-01T00:00:00Z', grantedScopes: [], reconnectRequired: false } })
    if (path.endsWith('/workspaces')) return Response.json({ workspaces: [] })
    if (path.endsWith('/private-events')) return Response.json({ events: [event] })
    if (path.endsWith('/projection')) return Response.json({ segments: [] })
    return new Response(null, { status: 404 })
  })
  window.history.replaceState({}, '', '/?auth=success')
  const view = render(<App />)
  return { routes, fetch, view }
}
afterEach(() => { cleanup(); vi.useRealTimers(); vi.restoreAllMocks(); window.history.replaceState({}, '', '/') })

it.each(['fetch', 'body', 'projection'])('recovers from stalled %s, discarding old period data', async phase => {
  const h = setup(); await screen.findByText(event.title)
  const late = deferred<Response>(), body = deferred<object>(), response = Response.json({}); response.json = () => body.promise
  const path = phase === 'projection' ? '/api/v1/people/owner/projection' : '/api/v1/me/private-events'
  h.routes.set(path, () => phase === 'fetch' ? late.promise : Promise.resolve(response))
  vi.useFakeTimers()
  await act(async () => { fireEvent.click(screen.getByRole('button', { name: '次の日' })) })
  expect(screen.queryByText(event.title)).not.toBeInTheDocument()
  await act(async () => { await vi.advanceTimersByTimeAsync(20_000) })
  expect(screen.getByRole('alert')).toHaveTextContent('本人用カレンダーを取得できませんでした。')
  h.routes.set('/api/v1/me/private-events', async () => Response.json({ events: [] }))
  h.routes.set('/api/v1/people/owner/projection', async () => Response.json({ segments: [] }))
  await act(async () => { fireEvent.click(screen.getByRole('button', { name: '本人用カレンダーを再読み込み' })) })
  await act(async () => { late.resolve(Response.json({ events: [event] })); body.resolve(phase === 'projection' ? { segments: [] } : { events: [event] }) })
  expect(screen.queryByText(event.title)).not.toBeInTheDocument()
  expect(screen.getByText('この期間の予定はありません。')).toBeInTheDocument()
  h.view.unmount(); expect(vi.getTimerCount()).toBe(0)
})

it('aborts pending reads on unmount', async () => {
  const h = setup(); await screen.findByText(event.title)
  h.routes.set('/api/v1/me/private-events', () => new Promise(() => {}))
  await act(async () => { fireEvent.click(screen.getByRole('button', { name: '次の日' })) })
  const call = h.fetch.mock.calls.filter(([url]) => String(url).includes('/private-events')).at(-1)
  h.view.unmount()
  expect(call?.[1]?.signal?.aborted).toBe(true)
})

it('reports invalid event formatting without crashing the calendar', async () => {
  const h = setup(); await screen.findByText(event.title)
  h.routes.set('/api/v1/me/private-events', async () => Response.json({ events: [{ ...event, allDay: false, startAt: 'bad-date' }] }))
  fireEvent.click(screen.getByRole('button', { name: '次の日' }))
  expect(await screen.findByRole('alert')).toHaveTextContent('本人用カレンダーを取得できませんでした。')
})
