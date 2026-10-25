import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import App from './App'

const snapshot = { id: 'r-notice', organizationId: 'org', requesterUserId: 'peer', targetUserId: 'actor', title: '通知から開いた依頼', type: 'meeting', priority: 'normal', status: 'suggested', durationMinutes: 30, deadlineAt: '2099-01-02T00:00:00Z', createdAt: '2030-01-01T00:00:00Z', options: [] }
const note = { id: 'n1', requestId: snapshot.id, type: 'request_changed', message: '対象の依頼が更新されました', createdAt: '2030-01-02T00:00:00Z' }
const json = (value: unknown) => new Response(JSON.stringify(value))
async function setup(detail: (id: string) => Promise<Response>, notes = [note]) {
  window.history.replaceState({}, '', '/?auth=success')
  const mock = vi.spyOn(globalThis, 'fetch').mockImplementation(async input => {
    const url = String(input)
    if (url.endsWith('/auth/session')) return json({ authenticated: true, user: { userId: 'actor', organizationId: 'org', displayName: '本人', email: 'actor@example.com', role: 'OWNER' } })
    if (url.endsWith('/calendar/connection')) return json({ connected: false })
    if (url.endsWith('/workspaces')) return json({ activeWorkspaceId: 'org', workspaces: [{ id: 'org', name: '組織', role: 'OWNER' }] })
    if (url.endsWith('/auth/logout')) return new Response(null, { status: 204 })
    if (url.endsWith('/notifications')) return json({ notifications: notes })
    if (url.endsWith('/requests')) return json({ requests: [{ ...snapshot, id: 'existing', title: '以前の一覧' }] })
    if (url.includes('/requests/')) return detail(url.split('/').at(-1)!)
    return new Response('{}', { status: 404 })
  })
  render(<App />)
  await screen.findByRole('button', { name: '本人のアカウントメニュー' })
  fireEvent.click(screen.getByRole('button', { name: '通知' }))
  await screen.findByRole('button', { name: `依頼を開く: ${note.message}` })
  return mock
}
afterEach(() => { cleanup(); vi.restoreAllMocks(); window.history.replaceState({}, '', '/') })

it.each(['target', 'requester'])('opens an authorized %s request without marking read or mutating it', async role => {
  const value = role === 'requester' ? { ...snapshot, requesterUserId: 'actor', targetUserId: 'peer' } : snapshot
  const mock = await setup(async () => json(value))
  fireEvent.click(screen.getByRole('button', { name: `依頼を開く: ${note.message}` }))
  await screen.findByText(snapshot.title)
  expect(screen.getByRole('searchbox', { name: '依頼を検索' })).toHaveValue(snapshot.id)
  expect(screen.getByRole('button', { name: role === 'requester' ? '送信済み' : '依頼' })).toHaveAttribute('aria-current', 'page')
  expect(mock).toHaveBeenCalledWith(expect.stringContaining('/requests/r-notice'), expect.objectContaining({ headers: { 'X-Demo-User-ID': 'actor', 'X-Organization-ID': 'org' } }))
  expect(mock.mock.calls.some(([, init]) => init?.method === 'POST')).toBe(false)
})

it.each(['forbidden', 'foreign-org', 'wrong-id', 'wrong-participant', 'network'])('preserves the current list on %s and allows retry', async mode => {
  let fail = true
  await setup(async () => {
    if (!fail) return json(snapshot)
    if (mode === 'network') throw new Error('private diagnostic')
    if (mode === 'forbidden') return new Response('{}', { status: 403 })
    return json({ ...snapshot, ...(mode === 'foreign-org' ? { organizationId: 'other' } : mode === 'wrong-id' ? { id: 'other' } : { targetUserId: 'other' }) })
  })
  fireEvent.click(screen.getByRole('button', { name: '依頼' }))
  await screen.findByText('以前の一覧')
  fireEvent.click(screen.getByRole('button', { name: `依頼を開く: ${note.message}` }))
  await screen.findByText(/依頼を開けませんでした/)
  expect(screen.getByText('以前の一覧')).toBeInTheDocument()
  expect(screen.queryByText(snapshot.title)).not.toBeInTheDocument()
  fail = false
  fireEvent.click(screen.getByRole('button', { name: `依頼を開く: ${note.message}` }))
  await screen.findByText(snapshot.title)
})

it.each(['navigation', 'logout'])('discards a delayed detail after %s', async mode => {
  let release!: (response: Response) => void
  await setup(() => new Promise(resolve => { release = resolve }))
  fireEvent.click(screen.getByRole('button', { name: `依頼を開く: ${note.message}` }))
  await waitFor(() => expect(release).toBeTypeOf('function'))
  if (mode === 'navigation') fireEvent.click(screen.getByRole('button', { name: 'マイカレンダー' }))
  else {
    fireEvent.click(screen.getByRole('button', { name: '本人のアカウントメニュー' }))
    fireEvent.click(screen.getByRole('button', { name: 'ログアウト' }))
    await screen.findByText('ログアウトしました。')
  }
  await act(async () => { release(json(snapshot)) })
  expect(screen.queryByText(snapshot.title)).not.toBeInTheDocument()
})

it('keeps the newest notification navigation when responses arrive in reverse order', async () => {
  let release!: (response: Response) => void
  const other = { ...note, id: 'n2', requestId: 'newer', message: '別の依頼' }
  await setup(id => id === 'newer' ? Promise.resolve(json({ ...snapshot, id, title: '新しく選んだ依頼' })) : new Promise(resolve => { release = resolve }), [note, other])
  fireEvent.click(screen.getByRole('button', { name: `依頼を開く: ${note.message}` }))
  await waitFor(() => expect(release).toBeTypeOf('function'))
  fireEvent.click(screen.getByRole('button', { name: '依頼を開く: 別の依頼' }))
  await screen.findByText('新しく選んだ依頼')
  await act(async () => { release(json(snapshot)) })
  expect(screen.queryByText(snapshot.title)).not.toBeInTheDocument()
  expect(screen.getByText('新しく選んだ依頼')).toBeInTheDocument()
})
