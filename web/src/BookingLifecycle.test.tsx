import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import App from './App'

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (error: Error) => void
  const promise = new Promise<T>((ok, fail) => { resolve = ok; reject = fail })
  return { promise, resolve, reject }
}

function setup(suggested = false, invitation = false) {
  const start = Date.now() + 86400000
  const value = {
    id: 'booking', organizationId: 'org', requesterUserId: 'peer', targetUserId: 'owner',
    title: 'Private booking', type: 'meeting', durationMinutes: 30,
    deadlineAt: new Date(start + 86400000).toISOString(), createdAt: new Date().toISOString(),
    priority: 'normal', status: suggested ? 'suggested' : 'accepted', acceptedOptionId: suggested ? '' : 'old',
    rescheduleProposal: { id: 'proposal-new', proposerUserId: 'peer', expectedOptionId: 'old', status: 'proposed' },
    options: [
      { id: 'old', type: 'meeting', startAt: new Date(start).toISOString(), endAt: new Date(start + 1800000).toISOString() },
      { id: 'proposal-new', type: 'meeting', startAt: new Date(start + 3600000).toISOString(), endAt: new Date(start + 5400000).toISOString() },
    ],
  }
  const routes = new Map<string, () => Promise<Response>>()
  const fetchMock = vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
    const url = new URL(String(input), 'http://localhost')
    const key = `${init?.method ?? 'GET'} ${url.pathname}${url.search}`
    const custom = routes.get(key)
    if (custom) return custom()
    if (url.pathname.endsWith('/auth/session')) return Response.json({ authenticated: true, demoMode: false, user: { userId: 'owner', organizationId: 'org', displayName: 'Owner', email: 'owner@example.com', role: 'OWNER' } })
    if (url.pathname.endsWith('/auth/logout') || url.pathname.endsWith('/me/account')) return new Response(null, { status: 204 })
    if (url.pathname.endsWith('/calendar/connection')) return Response.json({ connected: false })
    if (url.pathname.endsWith('/workspaces')) return Response.json({ workspaces: [{ id: 'org', name: 'Personal', role: 'OWNER' }, { id: 'team', name: 'Team', role: 'OWNER' }] })
    if (url.pathname.endsWith('/workspaces/switch')) return Response.json({ activeWorkspace: { id: 'team', name: 'Team', role: 'OWNER' } })
    if (url.pathname.endsWith('/invitations/preview')) return Response.json({ invitationId: 'invite', organizationId: 'team', organizationName: 'Team', role: 'MEMBER', expiresAt: '2030-01-01T00:00:00Z' })
    if (url.pathname.endsWith('/invitations/accept')) return Response.json({ accepted: true })
    if (url.pathname.endsWith('/requests')) return Response.json({ requests: [value] })
    return new Response('{}', { status: 404 })
  })
  const create = vi.spyOn(URL, 'createObjectURL').mockReturnValue('blob:booking')
  const revoke = vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => undefined)
  const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => undefined)
  window.history.replaceState({}, '', invitation ? '/?auth=success&invite=synthetic-token' : '/?auth=success')
  return { value, routes, fetchMock, create, revoke, click, view: render(<App />) }
}

async function openRequests(name = '依頼') {
  await screen.findByRole('button', { name: 'Ownerのアカウントメニュー' })
  fireEvent.click(screen.getByRole('button', { name }))
  await screen.findByText('Private booking')
}

const notices = { logout: 'ログアウトしました。', delete: 'アカウントと保存データを削除しました。', switch: 'Team に切り替えました。' }
type Boundary = keyof typeof notices
async function changeScope(kind: Boundary, rejected = false) {
  fireEvent.click(screen.getByRole('button', { name: 'Ownerのアカウントメニュー' }))
  if (kind === 'switch') {
    fireEvent.change(await screen.findByRole('combobox', { name: 'Workspace' }), { target: { value: 'team' } })
  } else if (kind === 'logout') fireEvent.click(screen.getByRole('button', { name: 'ログアウト' }))
  else {
    fireEvent.click(screen.getByRole('button', { name: 'アカウントを削除' }))
    fireEvent.change(screen.getByLabelText('確認のため DELETE と入力'), { target: { value: 'DELETE' } })
    fireEvent.click(screen.getByRole('button', { name: '完全に削除する' }))
  }
  await screen.findByText(rejected
    ? kind === 'switch' ? 'Workspaceを切り替えられませんでした。' : kind === 'logout' ? 'ログアウトできませんでした。' : /共有Workspaceの最後のOWNERです/
    : notices[kind])
}

describe('Booking operations respect account and workspace lifetime', () => {
  afterEach(() => {
    vi.useRealTimers()
    vi.restoreAllMocks()
    document.querySelectorAll('a[download]').forEach(link => link.remove())
    window.history.replaceState({}, '', '/')
  })

  it.each(['reschedule', 'cancel-confirmed', 'calendar.ics'] as const)('recovers from a stalled %s response and ignores it during a newer retry', async action => {
    await checkDeadline(action, false)
  })

  it.each(['reschedule', 'cancel-confirmed', 'calendar.ics'] as const)('bounds stalled %s body decoding and ignores it during a newer retry', async action => {
    await checkDeadline(action, true)
  })

  async function checkDeadline(action: 'reschedule' | 'cancel-confirmed' | 'calendar.ics', stalledBody: boolean) {
    const h = setup()
    const pending = deferred<Response>()
    const body = deferred<object | Blob>()
    const route = `${action === 'calendar.ics' ? 'GET' : 'POST'} /api/v1/requests/booking/${action}`
    const result = action === 'cancel-confirmed' ? { id: 'booking', status: 'cancelled' } : { ...h.value, title: 'Updated booking', acceptedOptionId: 'proposal-new', rescheduleProposal: { ...h.value.rescheduleProposal, status: 'accepted' } }
    const payload = action === 'calendar.ics' ? new Response('synthetic ICS') : Response.json(result)
    if (stalledBody) {
      if (action === 'calendar.ics') payload.blob = () => body.promise as Promise<Blob>
      else payload.json = () => body.promise
    }
    h.routes.set(route, () => stalledBody ? Promise.resolve(payload) : pending.promise)
    await openRequests()
    const label = action === 'reschedule' ? 'この日時への変更を承認' : action === 'cancel-confirmed' ? '会議の取消を確定する' : 'カレンダーに登録（ICS）'
    if (action === 'cancel-confirmed') fireEvent.click(screen.getByRole('button', { name: '確定会議を取り消す' }))
    vi.useFakeTimers()
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: label })) })
    const signal = h.fetchMock.mock.calls.at(-1)?.[1]?.signal
    expect(signal?.aborted).toBe(false)
    await act(async () => { await vi.advanceTimersByTimeAsync(20_000) })
    expect(signal?.aborted).toBe(true)
    expect(screen.getByRole('alert')).toHaveTextContent(action === 'reschedule' ? '日時変更の結果を確認できませんでした' : action === 'cancel-confirmed' ? '取消を確認できませんでした' : '取得できませんでした')
    expect(screen.getByRole('button', { name: label })).toBeEnabled()
    expect(h.fetchMock.mock.calls.filter(([input]) => String(input).endsWith(`/${action}`))).toHaveLength(1)
    expect(h.create).not.toHaveBeenCalled()

    const retry = deferred<Response>()
    h.routes.set(route, () => retry.promise)
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: label })) })
    await act(async () => {
      if (stalledBody) body.resolve(action === 'calendar.ics' ? new Blob(['synthetic ICS']) : result)
      else pending.resolve(payload)
    })
    expect(screen.getByRole('button', { name: action === 'calendar.ics' ? '取得中…' : label })).toBeDisabled()
    expect(h.create).not.toHaveBeenCalled()
    expect(screen.queryByText('Updated booking')).not.toBeInTheDocument()
    expect(screen.queryByText('キャンセル済み')).not.toBeInTheDocument()
    await act(async () => { retry.resolve(action === 'calendar.ics' ? new Response('retry ICS') : Response.json(result)) })
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    if (action === 'calendar.ics') expect(h.click).toHaveBeenCalledTimes(1)
    else if (action === 'reschedule') expect(screen.getByText('Updated booking')).toBeInTheDocument()
    else expect(screen.queryByRole('button', { name: 'カレンダーに登録（ICS）' })).not.toBeInTheDocument()
    await act(async () => { await vi.advanceTimersByTimeAsync(1000) })
    expect(vi.getTimerCount()).toBe(0)
  }

  it.each([null, {}, { id: 'another', status: 'cancelled' }, { id: 'booking', status: 'accepted' }])('does not mark cancellation successful for invalid acknowledgement %j', async value => {
    const h = setup()
    h.routes.set('POST /api/v1/requests/booking/cancel-confirmed', async () => Response.json(value))
    await openRequests()
    fireEvent.click(screen.getByRole('button', { name: '確定会議を取り消す' }))
    fireEvent.click(screen.getByRole('button', { name: '会議の取消を確定する' }))
    await screen.findByText('取消を確認できませんでした。依頼を更新し、最新状態を確認して再試行してください。')
    expect(screen.getByRole('button', { name: 'カレンダーに登録（ICS）' })).toBeEnabled()
    expect(screen.queryByText('キャンセル済み')).not.toBeInTheDocument()
    h.routes.set('POST /api/v1/requests/booking/cancel-confirmed', async () => Response.json({ id: 'booking', status: 'cancelled' }))
    fireEvent.click(screen.getByRole('button', { name: '会議の取消を確定する' }))
    await screen.findByText('確定会議を取り消し、相手に通知しました。外部カレンダーの予定は手動で削除してください。')
    expect(screen.queryByRole('button', { name: 'カレンダーに登録（ICS）' })).not.toBeInTheDocument()
  })

  it.each(['logout', 'delete', 'switch', 'unmount'] as const)('does not download an ICS after %s during Blob decoding', async kind => {
    const h = setup()
    const response = deferred<Response>()
    const body = deferred<Blob>()
    const payload = new Response('synthetic ICS')
    payload.blob = () => body.promise
    h.routes.set('GET /api/v1/requests/booking/calendar.ics', () => response.promise)
    await openRequests()
    fireEvent.click(screen.getByRole('button', { name: 'カレンダーに登録（ICS）' }))
    await act(async () => { response.resolve(payload) })
    if (kind === 'unmount') h.view.unmount()
    else await changeScope(kind)
    await act(async () => { body.resolve(new Blob(['synthetic ICS'])) })
    expect(h.create).not.toHaveBeenCalled()
    expect(h.click).not.toHaveBeenCalled()
  })

  it.each(['logout', 'switch'] as const)('does not decode a late ICS response after %s', async kind => {
    const h = setup()
    const pending = deferred<Response>()
    const response = new Response('synthetic ICS')
    const blob = vi.spyOn(response, 'blob')
    h.routes.set('GET /api/v1/requests/booking/calendar.ics', () => pending.promise)
    await openRequests()
    fireEvent.click(screen.getByRole('button', { name: 'カレンダーに登録（ICS）' }))
    await changeScope(kind)
    await act(async () => { pending.resolve(response) })
    expect(blob).not.toHaveBeenCalled()
    expect(h.click).not.toHaveBeenCalled()
  })

  it.each(['accept', 'reschedule', 'cancel-confirmed'] as const)('suppresses late %s completion after workspace switch', async action => {
    const h = setup(action === 'accept')
    const pending = deferred<Response>()
    h.routes.set(`POST /api/v1/requests/booking/${action}`, () => pending.promise)
    await openRequests()
    if (action === 'accept') fireEvent.click(screen.getAllByRole('button', { name: 'この候補を承認' })[0])
    else if (action === 'reschedule') fireEvent.click(screen.getByRole('button', { name: 'この日時への変更を承認' }))
    else {
      fireEvent.click(screen.getByRole('button', { name: '確定会議を取り消す' }))
      fireEvent.click(screen.getByRole('button', { name: '会議の取消を確定する' }))
    }
    await changeScope('switch')
    await act(async () => { pending.resolve(Response.json({ ...h.value, title: 'Stale secret', acceptedOptionId: 'proposal-new' })) })
    expect(screen.getByRole('status')).toHaveTextContent(notices.switch)
    expect(screen.queryByText('Private booking')).not.toBeInTheDocument()
    expect(screen.queryByText('Stale secret')).not.toBeInTheDocument()
  })

  it.each(['logout', 'delete'] as const)('suppresses reschedule completion after %s during JSON decoding', async kind => {
    const h = setup()
    const body = deferred<object>()
    const response = Response.json({})
    response.json = () => body.promise
    h.routes.set('POST /api/v1/requests/booking/reschedule', async () => response)
    await openRequests()
    fireEvent.click(screen.getByRole('button', { name: 'この日時への変更を承認' }))
    await changeScope(kind)
    await act(async () => { body.resolve({ ...h.value, acceptedOptionId: 'proposal-new' }) })
    expect(screen.getByRole('status')).toHaveTextContent(notices[kind])
  })

  it('ignores late acceptance conflict details after logout', async () => {
    const h = setup(true)
    const body = deferred<object>()
    const response = new Response('{}', { status: 409 })
    response.json = () => body.promise
    h.routes.set('POST /api/v1/requests/booking/accept', async () => response)
    await openRequests()
    fireEvent.click(screen.getAllByRole('button', { name: 'この候補を承認' })[0])
    await changeScope('logout')
    await act(async () => { body.resolve({ code: 'booking_conflict' }) })
    expect(screen.getByRole('status')).toHaveTextContent(notices.logout)
  })

  it.each(['依頼', '送信済み'])('discards a late %s list after switching workspaces', async name => {
    const h = setup()
    const pending = deferred<Response>()
    h.routes.set('GET /api/v1/requests' + (name === '送信済み' ? '?scope=sent' : ''), () => pending.promise)
    await screen.findByRole('button', { name: 'Ownerのアカウントメニュー' })
    fireEvent.click(screen.getByRole('button', { name }))
    await changeScope('switch')
    await act(async () => { pending.resolve(Response.json({ requests: [h.value] })) })
    expect(screen.queryByText('Private booking')).not.toBeInTheDocument()
    expect(screen.getByText('「更新」で依頼を取得してください。')).toBeInTheDocument()
  })

  it.each(['依頼', '送信済み'])('keeps the latest %s refresh when the first response arrives last', async name => {
    const h = setup()
    const pending = deferred<Response>()
    let calls = 0
    h.routes.set('GET /api/v1/requests' + (name === '送信済み' ? '?scope=sent' : ''), () => ++calls === 1 ? pending.promise : Promise.resolve(Response.json({ requests: [{ ...h.value, title: 'Latest booking' }] })))
    await screen.findByRole('button', { name: 'Ownerのアカウントメニュー' })
    fireEvent.click(screen.getByRole('button', { name }))
    fireEvent.click(screen.getByRole('button', { name: '更新' }))
    await screen.findByText('Latest booking')
    await act(async () => { pending.resolve(Response.json({ requests: [h.value] })) })
    expect(screen.getByText('Latest booking')).toBeInTheDocument()
    expect(screen.queryByText('Private booking')).not.toBeInTheDocument()
  })

  it.each(['logout', 'delete', 'switch'] as const)('preserves a pending ICS when %s is rejected', async kind => {
    const h = setup()
    const pending = deferred<Response>()
    h.routes.set('GET /api/v1/requests/booking/calendar.ics', () => pending.promise)
    h.routes.set(kind === 'switch' ? 'POST /api/v1/workspaces/switch' : kind === 'logout' ? 'POST /api/v1/auth/logout' : 'DELETE /api/v1/me/account', async () => new Response(null, { status: kind === 'delete' ? 409 : 503 }))
    await openRequests()
    fireEvent.click(screen.getByRole('button', { name: 'カレンダーに登録（ICS）' }))
    await changeScope(kind, true)
    await act(async () => { pending.resolve(new Response('synthetic ICS')) })
    expect(h.click).toHaveBeenCalledTimes(1)
    await waitFor(() => expect(h.revoke).toHaveBeenCalledWith('blob:booking'), { timeout: 2000 })
  })

  it('releases the temporary anchor and URL if the ICS download click fails', async () => {
    const h = setup()
    h.routes.set('GET /api/v1/requests/booking/calendar.ics', async () => new Response('synthetic ICS'))
    h.click.mockImplementation(() => { throw new Error('synthetic blocked click') })
    await openRequests()
    fireEvent.click(screen.getByRole('button', { name: 'カレンダーに登録（ICS）' }))
    await screen.findByText('取得できませんでした。依頼を更新して再試行してください。')
    expect(document.querySelector('a[download]')).toBeNull()
    await waitFor(() => expect(h.revoke).toHaveBeenCalledWith('blob:booking'), { timeout: 2000 })
  })

  it.each(['依頼', '送信済み'])('discards %s JSON decoded after workspace switch', async name => {
    const h = setup()
    const body = deferred<object>()
    const response = Response.json({})
    response.json = () => body.promise
    h.routes.set('GET /api/v1/requests' + (name === '送信済み' ? '?scope=sent' : ''), async () => response)
    await screen.findByRole('button', { name: 'Ownerのアカウントメニュー' })
    fireEvent.click(screen.getByRole('button', { name }))
    await changeScope('switch')
    await act(async () => { body.resolve({ requests: [h.value] }) })
    expect(screen.queryByText('Private booking')).not.toBeInTheDocument()
    expect(screen.getByText('「更新」で依頼を取得してください。')).toBeInTheDocument()
  })

  it.each(['依頼', '送信済み'])('does not let an old %s error clear the current loading state', async name => {
    const h = setup()
    const first = deferred<Response>()
    const second = deferred<Response>()
    let calls = 0
    h.routes.set('GET /api/v1/requests' + (name === '送信済み' ? '?scope=sent' : ''), () => ++calls === 1 ? first.promise : second.promise)
    await screen.findByRole('button', { name: 'Ownerのアカウントメニュー' })
    fireEvent.click(screen.getByRole('button', { name }))
    fireEvent.click(screen.getByRole('button', { name: '更新' }))
    await act(async () => { first.reject(new Error('obsolete load')) })
    expect(screen.getByText(name === '依頼' ? '依頼を取得しています…' : '送信済み依頼を取得しています…')).toBeInTheDocument()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    await act(async () => { second.resolve(Response.json({ requests: [h.value] })) })
    expect(screen.getByText('Private booking')).toBeInTheDocument()
  })

  it('does not revive an old ICS when switching away and back to the original workspace', async () => {
    const h = setup()
    const pending = deferred<Response>()
    h.routes.set('GET /api/v1/requests/booking/calendar.ics', () => pending.promise)
    await openRequests()
    fireEvent.click(screen.getByRole('button', { name: 'カレンダーに登録（ICS）' }))
    await changeScope('switch')
    h.routes.set('POST /api/v1/workspaces/switch', async () => Response.json({ activeWorkspace: { id: 'org', name: 'Personal', role: 'OWNER' } }))
    fireEvent.change(screen.getByRole('combobox', { name: 'Workspace' }), { target: { value: 'org' } })
    await screen.findByText('Personal に切り替えました。')
    await act(async () => { pending.resolve(new Response('synthetic ICS')) })
    expect(h.create).not.toHaveBeenCalled()
  })

  it('preserves a current reschedule when workspace switching fails', async () => {
    const h = setup()
    const pending = deferred<Response>()
    h.routes.set('POST /api/v1/requests/booking/reschedule', () => pending.promise)
    h.routes.set('POST /api/v1/workspaces/switch', async () => new Response(null, { status: 503 }))
    await openRequests()
    fireEvent.click(screen.getByRole('button', { name: 'この日時への変更を承認' }))
    await changeScope('switch', true)
    await act(async () => { pending.resolve(Response.json({ ...h.value, acceptedOptionId: 'proposal-new' })) })
    expect(screen.getByRole('status')).toHaveTextContent('日時変更を確定しました。')
    expect(screen.getByText('Private booking')).toBeInTheDocument()
  })

  it('invalidates pending ICS when an accepted invitation switches the workspace', async () => {
    const h = setup(false, true)
    const pending = deferred<Response>()
    h.routes.set('GET /api/v1/requests/booking/calendar.ics', () => pending.promise)
    await openRequests()
    fireEvent.click(screen.getByRole('button', { name: 'カレンダーに登録（ICS）' }))
    fireEvent.click(screen.getByRole('button', { name: 'Ownerのアカウントメニュー' }))
    fireEvent.click(await screen.findByRole('button', { name: '招待を受諾' }))
    await screen.findByText('Team に参加しました。')
    await act(async () => { pending.resolve(new Response('synthetic ICS')) })
    expect(h.create).not.toHaveBeenCalled()
    expect(screen.queryByText('Private booking')).not.toBeInTheDocument()
  })
})
