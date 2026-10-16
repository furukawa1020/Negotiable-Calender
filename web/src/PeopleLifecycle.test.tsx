import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import App from './App'

function deferred<T>() { let resolve!: (v: T) => void; const promise = new Promise<T>(done => { resolve = done }); return { promise, resolve } }
const person = { id: 'private-member', displayName: '旧組織の非公開メンバー', timezone: 'Asia/Tokyo', role: 'MANAGER' }
function setup() {
  const routes = new Map<string, () => Promise<Response>>()
  const fetch = vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
    const path = new URL(String(input), 'http://localhost').pathname
    if (routes.has(path)) return routes.get(path)!()
    if (path.endsWith('/auth/session')) return Response.json({ authenticated: true, user: { userId: 'owner', organizationId: 'org', displayName: 'Owner', email: '', role: 'OWNER' } })
    if (path.endsWith('/auth/logout')) return new Response(null, { status: 204 })
    if (path.endsWith('/calendar/connection')) return Response.json({ connected: false })
    if (path.endsWith('/workspaces')) return Response.json({ workspaces: [{ id: 'org', name: 'Personal', role: 'OWNER' }, { id: 'team', name: 'Team', role: 'OWNER' }] })
    if (path.endsWith('/workspaces/switch')) return Response.json({ activeWorkspace: { id: JSON.parse(String(init?.body)).organizationId, name: 'Team', role: 'OWNER' } })
    if (path.endsWith('/people')) return Response.json({ people: [person] })
    if (path.endsWith('/projection')) return Response.json({ segments: [] })
    return new Response(null, { status: 404 })
  })
  window.history.replaceState({}, '', '/?auth=success')
  render(<App />)
  return { routes, fetch }
}
afterEach(() => { cleanup(); vi.useRealTimers(); vi.restoreAllMocks(); window.history.replaceState({}, '', '/') })
async function openPeople() {
  await screen.findByRole('button', { name: 'Ownerのアカウントメニュー' })
  fireEvent.click(screen.getByRole('button', { name: '組織' }))
}
async function changeScope(boundary: string) {
  fireEvent.click(screen.getByRole('button', { name: 'Ownerのアカウントメニュー' }))
  if (boundary === 'logout') fireEvent.click(screen.getByRole('button', { name: 'ログアウト' }))
  else fireEvent.change(await screen.findByRole('combobox', { name: 'Workspace' }), { target: { value: 'team' } })
  await screen.findByText(boundary === 'logout' ? 'ログアウトしました。' : 'Team に切り替えました。')
}

it.each(['logout', 'switch'])('discards late member projection after %s', async boundary => {
  const h = setup(), body = deferred<object>(), response = Response.json({}); response.json = () => body.promise
  h.routes.set('/api/v1/people/private-member/projection', async () => response)
  await openPeople()
  await waitFor(() => expect(h.fetch.mock.calls.some(([url]) => String(url).includes('/private-member/projection'))).toBe(true))
  await changeScope(boundary)
  await act(async () => { body.resolve({ segments: [] }) })
  expect(screen.queryByText(person.displayName)).not.toBeInTheDocument()
  expect(screen.queryByText('公開状態を取得しています…')).not.toBeInTheDocument()
})

it('clears already displayed people when switching workspace', async () => {
  setup(); await openPeople(); await screen.findByText(person.displayName)
  await changeScope('switch')
  expect(screen.queryByText(person.displayName)).not.toBeInTheDocument()
})

it.each(['directory', 'projection'])('bounds stalled %s and prevents old responses overwriting refreshed data', async phase => {
  const h = setup(), body = deferred<object>(), response = Response.json({}); response.json = () => body.promise
  const path = phase === 'directory' ? '/api/v1/people' : '/api/v1/people/private-member/projection'
  h.routes.set(path, async () => response)
  await screen.findByRole('button', { name: 'Ownerのアカウントメニュー' })
  vi.useFakeTimers()
  await act(async () => { fireEvent.click(screen.getByRole('button', { name: '組織' })) })
  await act(async () => { await vi.advanceTimersByTimeAsync(20_000) })
  expect(screen.getByRole('alert')).toHaveTextContent('組織の公開状態を取得できませんでした。')
  h.routes.set('/api/v1/people', async () => Response.json({ people: [] }))
  await act(async () => { fireEvent.click(screen.getByRole('button', { name: '更新' })) })
  await act(async () => { body.resolve(phase === 'directory' ? { people: [person] } : { segments: [] }) })
  expect(screen.queryByText(person.displayName)).not.toBeInTheDocument()
  expect(screen.getByText('表示できる管理職はいません。')).toBeInTheDocument()
  expect(vi.getTimerCount()).toBe(0)
})

it.each([{ people: null }, { people: [{ id: 'bad' }] }, { people: [person, person] }])('rejects malformed or duplicate directory entries', async payload => {
  const h = setup(); h.routes.set('/api/v1/people', async () => Response.json(payload))
  await openPeople()
  expect(await screen.findByRole('alert')).toHaveTextContent('組織の公開状態を取得できませんでした。')
  expect(screen.queryByText(person.displayName)).not.toBeInTheDocument()
})
