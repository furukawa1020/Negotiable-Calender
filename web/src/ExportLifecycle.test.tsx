import { act, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import App from './App'

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (error: Error) => void
  const promise = new Promise<T>((ok, fail) => { resolve = ok; reject = fail })
  return { promise, resolve, reject }
}

function setup(exportResponse: () => Promise<Response>, deletionStatus = 204) {
  window.history.replaceState({}, '', '/?auth=success')
  vi.spyOn(globalThis, 'fetch').mockImplementation(async input => {
    const url = String(input)
    if (url.endsWith('/auth/session')) return Response.json({ authenticated: true, demoMode: false, user: { userId: 'owner', organizationId: 'org', displayName: 'Owner', email: 'owner@example.com', role: 'OWNER' } })
    if (url.endsWith('/calendar/connection')) return Response.json({ connected: false })
    if (url.endsWith('/workspaces')) return Response.json({ workspaces: [] })
    if (url.endsWith('/auth/logout')) return new Response(null, { status: 204 })
    if (url.endsWith('/me/account')) return new Response(null, { status: deletionStatus })
    if (url.endsWith('/export')) return exportResponse()
    return new Response('{}', { status: 404 })
  })
  const create = vi.spyOn(URL, 'createObjectURL').mockReturnValue('blob:private-export')
  const revoke = vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => undefined)
  const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => undefined)
  return { view: render(<App />), create, revoke, click }
}

async function startExport() {
  fireEvent.click(await screen.findByRole('button', { name: 'Ownerのアカウントメニュー' }))
  fireEvent.click(screen.getByRole('button', { name: '本人データをエクスポート' }))
}

async function deleteAccount() {
  fireEvent.click(screen.getByRole('button', { name: 'アカウントを削除' }))
  fireEvent.change(screen.getByLabelText('確認のため DELETE と入力'), { target: { value: 'DELETE' } })
  fireEvent.click(screen.getByRole('button', { name: '完全に削除する' }))
}

describe('Personal export lifetime', () => {
  afterEach(() => {
    vi.restoreAllMocks()
    document.querySelectorAll('a[download]').forEach(link => link.remove())
    window.history.replaceState({}, '', '/')
  })

  it.each([
    ['logout', 'response'], ['logout', 'blob'], ['delete', 'response'],
    ['delete', 'blob'], ['unmount', 'response'], ['unmount', 'blob'],
  ] as const)('does not start a download after %s during %s wait', async (kind, stage) => {
    const pendingResponse = deferred<Response>()
    const pendingBlob = deferred<Blob>()
    const response = Response.json({ privateData: 'synthetic' })
    response.blob = () => pendingBlob.promise
    const h = setup(() => stage === 'response' ? pendingResponse.promise : Promise.resolve(response))
    await startExport()
    if (kind === 'unmount') h.view.unmount()
    else if (kind === 'logout') {
      fireEvent.click(screen.getByRole('button', { name: 'ログアウト' }))
      await screen.findByText('ログアウトしました。')
    } else {
      await deleteAccount()
      await screen.findByText('アカウントと保存データを削除しました。')
    }
    await act(async () => {
      pendingResponse.resolve(response)
      pendingBlob.resolve(new Blob(['synthetic private data']))
    })
    expect(h.create).not.toHaveBeenCalled()
    expect(h.click).not.toHaveBeenCalled()
    if (kind !== 'unmount') expect(screen.getByRole('status')).toHaveTextContent(kind === 'logout' ? 'ログアウトしました。' : 'アカウントと保存データを削除しました。')
  })

  it('does not overwrite logout with a late export error', async () => {
    const pending = deferred<Response>()
    const h = setup(() => pending.promise)
    await startExport()
    fireEvent.click(screen.getByRole('button', { name: 'ログアウト' }))
    await screen.findByText('ログアウトしました。')
    await act(async () => { pending.reject(new Error('synthetic private error')) })
    expect(screen.getByRole('status')).toHaveTextContent('ログアウトしました。')
    expect(h.create).not.toHaveBeenCalled()
  })

  it('always releases its object URL and temporary anchor when download click fails', async () => {
    const h = setup(async () => Response.json({ privateData: 'synthetic' }))
    h.click.mockImplementation(() => { throw new Error('blocked download') })
    await startExport()
    await screen.findByText('データをエクスポートできませんでした。')
    expect(h.revoke).toHaveBeenCalledWith('blob:private-export')
    expect(document.querySelector('a[download]')).toBeNull()
  })

  it('keeps a current export valid when account deletion is rejected', async () => {
    const pending = deferred<Response>()
    const h = setup(() => pending.promise, 409)
    await startExport()
    await deleteAccount()
    await screen.findByText(/共有Workspaceの最後のOWNERです/)
    await act(async () => { pending.resolve(Response.json({ privateData: 'synthetic' })) })
    expect(h.click).toHaveBeenCalledTimes(1)
    expect(h.revoke).toHaveBeenCalledWith('blob:private-export')
    expect(screen.getByText('本人データを安全にエクスポートしました。')).toBeInTheDocument()
  })

  it.each([true, false])('discards a pre-session demo export after session resolution (authenticated=%s)', async authenticated => {
    window.history.replaceState({}, '', '/?auth=success')
    const pendingSession = deferred<Response>()
    const pendingExport = deferred<Response>()
    vi.spyOn(globalThis, 'fetch').mockImplementation(async input => {
      const url = String(input)
      if (url.endsWith('/auth/session')) return pendingSession.promise
      if (url.endsWith('/export')) return pendingExport.promise
      if (url.endsWith('/calendar/connection')) return Response.json({ connected: false })
      if (url.endsWith('/workspaces')) return Response.json({ workspaces: [] })
      return new Response('{}', { status: 404 })
    })
    const create = vi.spyOn(URL, 'createObjectURL').mockReturnValue('blob:obsolete-demo')
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => undefined)
    vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => undefined)
    render(<App />)
    fireEvent.click(screen.getByRole('button', { name: '山田 太郎のアカウントメニュー' }))
    fireEvent.click(screen.getByRole('button', { name: '本人データをエクスポート' }))
    await act(async () => { pendingSession.resolve(Response.json({ authenticated, demoMode: false, user: { userId: 'owner', organizationId: 'org', displayName: 'Owner', email: 'owner@example.com', role: 'OWNER' } })) })
    await act(async () => { pendingExport.resolve(Response.json({ privateData: 'obsolete demo' })) })
    expect(create).not.toHaveBeenCalled()
    expect(click).not.toHaveBeenCalled()
    if (authenticated) {
      expect(screen.getByRole('button', { name: 'Ownerのアカウントメニュー' })).toBeInTheDocument()
      expect(screen.getByRole('button', { name: '本人データをエクスポート' })).toBeEnabled()
    }
    else expect(screen.getByRole('heading', { name: 'カレンダーにログイン' })).toBeInTheDocument()
  })

  it('does not download a pending demo export after unmount', async () => {
    window.history.replaceState({}, '', '/')
    const pending = deferred<Response>()
    vi.spyOn(globalThis, 'fetch').mockImplementation(() => pending.promise)
    const create = vi.spyOn(URL, 'createObjectURL').mockReturnValue('blob:obsolete-demo')
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => undefined)
    vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => undefined)
    const view = render(<App />)
    fireEvent.click(screen.getByRole('button', { name: '山田 太郎のアカウントメニュー' }))
    fireEvent.click(screen.getByRole('button', { name: '本人データをエクスポート' }))
    view.unmount()
    await act(async () => { pending.resolve(Response.json({ privateData: 'obsolete demo' })) })
    expect(create).not.toHaveBeenCalled()
    expect(click).not.toHaveBeenCalled()
  })
})
