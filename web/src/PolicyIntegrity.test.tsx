import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import App from './App'
import { parseSharingPolicy } from './sharingPolicy'

const policy = { default: { availability: 'unavailable', interruptibility: 'do_not_interrupt', requestability: 'closed', reschedulability: 'fixed' }, workingHours: [], rules: [] }
function deferred<T>() { let resolve!: (v: T) => void; const promise = new Promise<T>(done => { resolve = done }); return { promise, resolve } }
afterEach(() => { cleanup(); vi.useRealTimers(); vi.restoreAllMocks(); window.history.replaceState({}, '', '/') })

it('preserves empty working hours through load and save', async () => {
  const fetch = vi.spyOn(globalThis, 'fetch').mockImplementation(async () => Response.json(policy))
  render(<App />)
  fireEvent.click(screen.getByRole('button', { name: '共有ルールを確認' }))
  const save = screen.getByRole('button', { name: '保存して公開状態を更新' })
  await waitFor(() => expect(save).toBeEnabled())
  expect(screen.queryByLabelText('月曜日 勤務開始 1')).not.toBeInTheDocument()
  fireEvent.click(save)
  await screen.findByText('共有ルールを保存しました。')
  expect(JSON.parse(String(fetch.mock.calls[1][1]?.body)).workingHours).toEqual([])
})

it.each([new Response('{}', { status: 500 }), Response.json({}), Response.json({ ...policy, workingHours: [{ weekday: 1, startMinute: 'bad', endMinute: 600 }] })])('blocks edits after unreadable policy and allows explicit reload', async response => {
  vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(response).mockImplementation(async () => Response.json(policy))
  render(<App />)
  fireEvent.click(screen.getByRole('button', { name: '共有ルールを確認' }))
  await screen.findByRole('alert')
  expect(screen.getByRole('button', { name: '保存して公開状態を更新' })).toBeDisabled()
  expect(screen.getByLabelText('基本の公開状態')).toBeDisabled()
  fireEvent.click(screen.getByRole('button', { name: '共有ルールを再読み込み' }))
  await waitFor(() => expect(screen.getByLabelText('基本の公開状態')).toBeEnabled())
  expect(screen.getByLabelText('基本の公開状態')).toHaveValue('unavailable')
})

it.each(['fetch', 'body'] as const)('bounds policy %s and ignores its late result after reload', async phase => {
  const late = deferred<Response>(), body = deferred<object>()
  const response = Response.json({}); response.json = () => body.promise
  const fetch = vi.spyOn(globalThis, 'fetch').mockImplementationOnce(() => phase === 'fetch' ? late.promise : Promise.resolve(response)).mockImplementation(async () => Response.json(policy))
  render(<App />)
  vi.useFakeTimers()
  await act(async () => { fireEvent.click(screen.getByRole('button', { name: '共有ルールを確認' })) })
  await act(async () => { await vi.advanceTimersByTimeAsync(20_000) })
  expect(fetch.mock.calls[0][1]?.signal?.aborted).toBe(true)
  expect(screen.getByRole('button', { name: '保存して公開状態を更新' })).toBeDisabled()
  await act(async () => { fireEvent.click(screen.getByRole('button', { name: '共有ルールを再読み込み' })) })
  await act(async () => { late.resolve(Response.json({ ...policy, default: { ...policy.default, availability: 'available' } })); body.resolve({ ...policy, default: { ...policy.default, availability: 'available' } }) })
  expect(screen.getByLabelText('基本の公開状態')).toHaveValue('unavailable')
  expect(vi.getTimerCount()).toBe(0)
})

it('requires reloading after an unknown save outcome instead of overwriting again', async () => {
  const body = deferred<object>(), response = Response.json({}); response.json = () => body.promise
  vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(Response.json(policy)).mockResolvedValueOnce(response)
  render(<App />)
  fireEvent.click(screen.getByRole('button', { name: '共有ルールを確認' }))
  const save = screen.getByRole('button', { name: '保存して公開状態を更新' })
  await waitFor(() => expect(save).toBeEnabled())
  vi.useFakeTimers()
  await act(async () => { fireEvent.click(save) })
  await act(async () => { await vi.advanceTimersByTimeAsync(20_000) })
  expect(screen.getByRole('alert')).toHaveTextContent('保存結果を確認できませんでした')
  expect(save).toBeDisabled()
  await act(async () => { body.resolve(policy) })
  expect(screen.queryByText('共有ルールを保存しました。')).not.toBeInTheDocument()
})

it('does not apply an earlier dialog read to a reopened editor', async () => {
  const body = deferred<object>(), response = Response.json({}); response.json = () => body.promise
  vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(response).mockResolvedValueOnce(Response.json(policy))
  render(<App />)
  fireEvent.click(screen.getByRole('button', { name: '共有ルールを確認' }))
  await act(async () => {})
  fireEvent.click(screen.getByRole('button', { name: '閉じる' }))
  fireEvent.click(screen.getByRole('button', { name: '共有ルールを確認' }))
  await waitFor(() => expect(screen.getByLabelText('基本の公開状態')).toBeEnabled())
  await act(async () => { body.resolve({ ...policy, default: { ...policy.default, availability: 'available' } }) })
  expect(screen.getByLabelText('基本の公開状態')).toHaveValue('unavailable')
})

it('accepts explicit nil slices, rejects missing fields and invalid enum states', () => {
  expect(parseSharingPolicy({ ...policy, workingHours: null, rules: null })).toEqual(policy)
  expect(() => parseSharingPolicy({ default: policy.default })).toThrow()
  expect(() => parseSharingPolicy({ ...policy, default: { ...policy.default, requestability: 'unsafe' } })).toThrow()
  expect(() => parseSharingPolicy({ ...policy, default: { ...policy.default, requestability: ['closed'] } })).toThrow()
})

it('allows initial configuration only when the API explicitly reports no saved policy', async () => {
  vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(null, { status: 404 }))
  render(<App />)
  fireEvent.click(screen.getByRole('button', { name: '共有ルールを確認' }))
  await waitFor(() => expect(screen.getByRole('button', { name: '保存して公開状態を更新' })).toBeEnabled())
  expect(screen.getByLabelText('月曜日 勤務開始 1')).toHaveValue('09:00')
})

it('discards a delayed policy read after logout', async () => {
  const body = deferred<object>(), response = Response.json({}); response.json = () => body.promise
  window.history.replaceState({}, '', '/?auth=success')
  vi.spyOn(globalThis, 'fetch').mockImplementation(async input => {
    const url = String(input)
    if (url.endsWith('/auth/session')) return Response.json({ authenticated: true, user: { userId: 'owner', organizationId: 'org', displayName: 'Owner', email: '', role: 'OWNER' } })
    if (url.endsWith('/calendar/connection')) return Response.json({ connected: false })
    if (url.endsWith('/workspaces')) return Response.json({ workspaces: [] })
    if (url.endsWith('/auth/logout')) return new Response(null, { status: 204 })
    if (url.endsWith('/sharing-policy')) return response
    return new Response(null, { status: 404 })
  })
  render(<App />)
  await screen.findByRole('button', { name: 'Ownerのアカウントメニュー' })
  fireEvent.click(screen.getByRole('button', { name: '共有ルールを確認' }))
  await act(async () => {})
  fireEvent.click(screen.getByRole('button', { name: 'Ownerのアカウントメニュー' }))
  fireEvent.click(screen.getByRole('button', { name: 'ログアウト' }))
  await screen.findByText('ログアウトしました。')
  await act(async () => { body.resolve(policy) })
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  expect(screen.getByRole('status')).toHaveTextContent('ログアウトしました。')
})
