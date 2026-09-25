import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { StrictMode } from 'react'
import App from './App'

const session = { authenticated: true, demoMode: false, user: { userId: 'owner', organizationId: 'org', displayName: 'Owner', email: 'owner@example.com', role: 'OWNER' } }
const connection = { connected: true, connection: { connectedAt: '2026-09-01T00:00:00Z', grantedScopes: [], reconnectRequired: false } }

describe('Account bootstrap teardown boundaries', () => {
  afterEach(() => { vi.restoreAllMocks(); window.history.replaceState({}, '', '/') })

  it('does not continue bootstrap after logout while connection decoding is delayed', async () => {
    window.history.replaceState({}, '', '/?auth=success&invite=old-token')
    let release: ((value: object) => void) | undefined
    const mock = vi.spyOn(globalThis, 'fetch').mockImplementation(async input => {
      const url = String(input)
      if (url.includes('/auth/session')) return Response.json(session)
      if (url.includes('/auth/logout')) return new Response(null, { status: 204 })
      if (url.includes('/calendar/connection')) {
        const response = Response.json({})
        response.json = () => new Promise(resolve => { release = resolve })
        return response
      }
      if (url.includes('/workspaces')) return Response.json({ workspaces: [] })
      return new Response('{}', { status: 404 })
    })
    render(<App />)
    fireEvent.click(await screen.findByRole('button', { name: 'Ownerのアカウントメニュー' }))
    await waitFor(() => expect(release).toBeTypeOf('function'))
    fireEvent.click(screen.getByRole('button', { name: 'ログアウト' }))
    expect(await screen.findByText('ログアウトしました。')).toBeInTheDocument()
    await act(async () => { release?.(connection) })
    expect(mock.mock.calls.some(([url]) => String(url).includes('/workspaces') || String(url).includes('/invitations/preview'))).toBe(false)
    expect(screen.getByRole('status')).toHaveTextContent('ログアウトしました。')
    expect(window.location.search).toBe('')
  })

  it('ignores an obsolete session after unmount without continuing requests or rewriting the new URL', async () => {
    window.history.replaceState({}, '', '/?auth=success')
    let release: ((value: object) => void) | undefined
    const mock = vi.spyOn(globalThis, 'fetch').mockImplementation(async () => {
      const response = Response.json({})
      response.json = () => new Promise(resolve => { release = resolve })
      return response
    })
    const view = render(<App />)
    await waitFor(() => expect(release).toBeTypeOf('function'))
    view.unmount()
    window.history.replaceState({}, '', '/new?invite=new-token')
    await act(async () => { release?.(session) })
    expect(mock).toHaveBeenCalledTimes(1)
    expect(window.location.search).toBe('?invite=new-token')
  })

  it.each([204, 409])('fences late sync only after successful deletion (status %s)', async status => {
    window.history.replaceState({}, '', '/?auth=success')
    let release: ((value: object) => void) | undefined
    vi.spyOn(globalThis, 'fetch').mockImplementation(async input => {
      const url = String(input)
      if (url.includes('/auth/session')) return Response.json(session)
      if (url.includes('/calendar/connection')) return Response.json(connection)
      if (url.includes('/calendar/sync')) {
        const response = Response.json({})
        response.json = () => new Promise(resolve => { release = resolve })
        return response
      }
      if (url.includes('/me/account')) return new Response(null, { status })
      if (url.includes('/workspaces')) return Response.json({ workspaces: [] })
      if (url.includes('/private-events')) return Response.json({ events: [] })
      if (url.includes('/projection')) return Response.json({ segments: [] })
      return new Response('{}', { status: 404 })
    })
    render(<App />)
    fireEvent.click(await screen.findByRole('button', { name: 'Ownerのアカウントメニュー' }))
    fireEvent.click(await screen.findByRole('button', { name: 'busy時間を同期' }))
    await waitFor(() => expect(release).toBeTypeOf('function'))
    fireEvent.click(screen.getByRole('button', { name: 'アカウントを削除' }))
    fireEvent.change(screen.getByLabelText('確認のため DELETE と入力'), { target: { value: 'DELETE' } })
    fireEvent.click(screen.getByRole('button', { name: '完全に削除する' }))
    if (status === 204) expect(await screen.findByText('アカウントと保存データを削除しました。')).toBeInTheDocument()
    else expect(await screen.findByText(/共有Workspaceの最後のOWNERです/)).toBeInTheDocument()
    await act(async () => { release?.({ busySpanCount: 99, lastSyncedAt: '2026-09-25T00:00:00Z' }) })
    if (status === 204) {
      expect(screen.getByRole('status')).toHaveTextContent('アカウントと保存データを削除しました。')
      expect(screen.queryByText(/99件/)).not.toBeInTheDocument()
    } else {
      expect(screen.getByRole('button', { name: 'Ownerのアカウントメニュー' })).toBeInTheDocument()
      expect(screen.getByText(/99件/)).toBeInTheDocument()
    }
  })

  it('does not replace logout with a late invitation transport failure', async () => {
    window.history.replaceState({}, '', '/?auth=success&invite=old-token')
    let rejectPreview: ((error: Error) => void) | undefined
    vi.spyOn(globalThis, 'fetch').mockImplementation(async input => {
      const url = String(input)
      if (url.includes('/auth/session')) return Response.json(session)
      if (url.includes('/auth/logout')) return new Response(null, { status: 204 })
      if (url.includes('/calendar/connection')) return Response.json({ connected: false })
      if (url.includes('/workspaces')) return Response.json({ workspaces: [] })
      if (url.includes('/invitations/preview')) return new Promise<Response>((_resolve, reject) => { rejectPreview = reject })
      return new Response('{}', { status: 404 })
    })
    render(<App />)
    await waitFor(() => expect(rejectPreview).toBeTypeOf('function'))
    fireEvent.click(screen.getByRole('button', { name: 'Ownerのアカウントメニュー' }))
    fireEvent.click(screen.getByRole('button', { name: 'ログアウト' }))
    expect(await screen.findByText('ログアウトしました。')).toBeInTheDocument()
    await act(async () => { rejectPreview?.(new Error('late private failure')) })
    expect(screen.getByRole('status')).toHaveTextContent('ログアウトしました。')
  })

  it('keeps the current StrictMode bootstrap when the cleaned-up request finishes later', async () => {
    window.history.replaceState({}, '', '/?auth=success')
    let releaseOld: ((value: object) => void) | undefined
    let sessions = 0
    let connections = 0
    vi.spyOn(globalThis, 'fetch').mockImplementation(async input => {
      const url = String(input)
      if (url.includes('/auth/session')) {
        if (++sessions > 1) return Response.json(session)
        const response = Response.json({})
        response.json = () => new Promise(resolve => { releaseOld = resolve })
        return response
      }
      if (url.includes('/calendar/connection')) { connections++; return Response.json({ connected: false }) }
      if (url.includes('/workspaces')) return Response.json({ workspaces: [] })
      return new Response('{}', { status: 404 })
    })
    render(<StrictMode><App /></StrictMode>)
    expect(await screen.findByRole('button', { name: 'Ownerのアカウントメニュー' })).toBeInTheDocument()
    await waitFor(() => expect(releaseOld).toBeTypeOf('function'))
    await act(async () => { releaseOld?.({ ...session, user: { ...session.user, displayName: 'Old identity' } }) })
    expect(screen.getByRole('button', { name: 'Ownerのアカウントメニュー' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Old identityのアカウントメニュー' })).not.toBeInTheDocument()
    expect(connections).toBe(1)
  })
})
