import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import App from './App'

function deferred<T>() { let resolve!: (v: T) => void; const promise = new Promise<T>(done => { resolve = done }); return { promise, resolve } }
afterEach(() => { cleanup(); vi.useRealTimers(); vi.restoreAllMocks(); window.history.replaceState({}, '', '/') })

it.each(['次の日', '週', '月', '今日'])('invalidates a pending preview when changing %s', async button => {
  const body = deferred<object>(), response = Response.json({}); response.json = () => body.promise
  vi.spyOn(globalThis, 'fetch').mockResolvedValue(response)
  render(<App />)
  fireEvent.click(screen.getByRole('button', { name: /メンバー表示をプレビュー/ }))
  await act(async () => {})
  fireEvent.click(screen.getByRole('button', { name: button }))
  await act(async () => { body.resolve({ segments: [] }) })
  expect(screen.queryByRole('button', { name: /自分の表示に戻る/ })).not.toBeInTheDocument()
  expect(screen.getByRole('button', { name: /メンバー表示をプレビュー/ })).toBeEnabled()
})

it('requests the selected week, and clears a displayed preview on navigation', async () => {
  const fetch = vi.spyOn(globalThis, 'fetch').mockResolvedValue(Response.json({ segments: [] }))
  render(<App />); fireEvent.click(screen.getByRole('button', { name: '週' }))
  fireEvent.click(screen.getByRole('button', { name: /メンバー表示をプレビュー/ }))
  await screen.findByRole('button', { name: /自分の表示に戻る/ })
  const query = new URL(String(fetch.mock.calls[0][0])).searchParams
  expect((Date.parse(query.get('to')!) - Date.parse(query.get('from')!)) / 86400000).toBe(7)
  fireEvent.click(screen.getByRole('button', { name: '次の週' }))
  expect(screen.queryByRole('button', { name: /自分の表示に戻る/ })).not.toBeInTheDocument()
})

it.each(['fetch', 'body'])('bounds preview %s and permits a new read', async phase => {
  const late = deferred<Response>(), body = deferred<object>(), response = Response.json({}); response.json = () => body.promise
  vi.spyOn(globalThis, 'fetch').mockImplementationOnce(() => phase === 'fetch' ? late.promise : Promise.resolve(response)).mockImplementation(async () => Response.json({ segments: [] }))
  render(<App />); vi.useFakeTimers()
  await act(async () => { fireEvent.click(screen.getByRole('button', { name: /メンバー表示をプレビュー/ })) })
  await act(async () => { await vi.advanceTimersByTimeAsync(20_000) })
  expect(screen.getByText('メンバー表示を取得できませんでした。')).toBeInTheDocument()
  await act(async () => { fireEvent.click(screen.getByRole('button', { name: /メンバー表示をプレビュー/ })) })
  expect(screen.getByRole('button', { name: /自分の表示に戻る/ })).toBeInTheDocument()
  await act(async () => { body.resolve({ segments: null }); late.resolve(Response.json({ segments: null })) })
  expect(screen.getByRole('button', { name: /自分の表示に戻る/ })).toBeInTheDocument()
  expect(vi.getTimerCount()).toBe(0)
})

it('does not re-enter preview after logout', async () => {
  const body = deferred<object>(), response = Response.json({}); response.json = () => body.promise
  window.history.replaceState({}, '', '/?auth=success')
  vi.spyOn(globalThis, 'fetch').mockImplementation(async input => {
    const path = String(input)
    if (path.includes('/auth/session')) return Response.json({ authenticated: true, user: { userId: 'owner', organizationId: 'org', displayName: 'Owner', email: '', role: 'OWNER' } })
    if (path.includes('/calendar/connection')) return Response.json({ connected: false })
    if (path.includes('/workspaces')) return Response.json({ workspaces: [] })
    if (path.includes('/auth/logout')) return new Response(null, { status: 204 })
    return response
  })
  render(<App />); await screen.findByRole('button', { name: 'Ownerのアカウントメニュー' })
  fireEvent.click(screen.getByRole('button', { name: /メンバー表示をプレビュー/ }))
  await act(async () => {})
  fireEvent.click(screen.getByRole('button', { name: 'Ownerのアカウントメニュー' })); fireEvent.click(screen.getByRole('button', { name: 'ログアウト' }))
  await screen.findByText('ログアウトしました。')
  await act(async () => { body.resolve({ segments: [] }) })
  expect(screen.getByRole('status')).toHaveTextContent('ログアウトしました。')
})
