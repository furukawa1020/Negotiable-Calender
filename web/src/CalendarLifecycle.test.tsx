import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import App from './App'

const session = {
  authenticated: true,
  user: { userId: 'owner', organizationId: 'org', displayName: 'Owner', email: 'owner@example.com', role: 'OWNER' },
}
const connection = { connected: true, connection: { connectedAt: '2026-09-01T00:00:00Z', grantedScopes: [], reconnectRequired: false } }

describe('Calendar connection lifecycle', () => {
  afterEach(() => {
    vi.restoreAllMocks()
    window.history.replaceState({}, '', '/')
  })

  it('discards a private read that completes during disconnect before effect cleanup', async () => {
    window.history.replaceState({}, '', '/?auth=success')
    let releasePrivate: ((value: object) => void) | undefined
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url = String(input)
      if (url.includes('/auth/session')) return Response.json(session)
      if (url.includes('/calendar/connection')) {
        if (init?.method !== 'DELETE') return Response.json(connection)
        const response = new Response(null, { status: 204 })
        // Resolve the old request in the same task as successful disconnect.
        // React has not run the read effect's cleanup when its continuation runs.
        Object.defineProperty(response, 'ok', { get: () => {
          releasePrivate?.({ events: [{ id: 'stale', title: 'Late private title', location: 'Late private location', startAt: '2026-09-21T00:00:00Z', endAt: '2026-09-21T01:00:00Z', allDay: false }] })
          return true
        } })
        return response
      }
      if (url.includes('/workspaces')) return Response.json({ workspaces: [] })
      if (url.includes('/private-events')) {
        const response = Response.json({})
        response.json = () => new Promise(resolve => { releasePrivate = resolve })
        return response
      }
      if (url.includes('/projection')) return Response.json({ segments: [{ startAt: '2026-09-21T00:00:00Z', endAt: '2026-09-21T01:00:00Z', availability: 'available', interruptibility: 'open', requestability: 'open', reschedulability: 'high' }] })
      return new Response('{}', { status: 404 })
    })
    render(<App />)
    fireEvent.click(await screen.findByRole('button', { name: 'Ownerのアカウントメニュー' }))
    await waitFor(() => expect(releasePrivate).toBeTypeOf('function'))
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Calendar接続を解除' })) })
    expect(await screen.findByText('Google Calendarの接続と同期済みbusy時間を削除しました。')).toBeInTheDocument()
    expect(screen.queryByText('Late private title')).not.toBeInTheDocument()
    expect(screen.queryByText('Late private location')).not.toBeInTheDocument()
    expect(screen.getByText(/表示できる公開状態はありません/)).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Google Calendarを接続' })).toBeInTheDocument()
  })

  it('does not overwrite logout with a late manual sync response', async () => {
    window.history.replaceState({}, '', '/?auth=success')
    let releaseSync: ((value: object) => void) | undefined
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)
      if (url.includes('/auth/session')) return Response.json(session)
      if (url.includes('/auth/logout')) return new Response(null, { status: 204 })
      if (url.includes('/calendar/connection')) return Response.json(connection)
      if (url.includes('/calendar/sync')) {
        const response = Response.json({})
        response.json = () => new Promise(resolve => { releaseSync = resolve })
        return response
      }
      if (url.includes('/workspaces')) return Response.json({ workspaces: [] })
      if (url.includes('/private-events')) return Response.json({ events: [] })
      if (url.includes('/projection')) return Response.json({ segments: [] })
      return new Response('{}', { status: 404 })
    })
    render(<App />)
    fireEvent.click(await screen.findByRole('button', { name: 'Ownerのアカウントメニュー' }))
    fireEvent.click(await screen.findByRole('button', { name: 'busy時間を同期' }))
    await waitFor(() => expect(releaseSync).toBeTypeOf('function'))
    fireEvent.click(screen.getByRole('button', { name: 'ログアウト' }))
    expect(await screen.findByText('ログアウトしました。')).toBeInTheDocument()
    await act(async () => { releaseSync?.({ busySpanCount: 99, lastSyncedAt: '2026-09-21T00:00:00Z' }) })
    expect(screen.getByRole('status')).toHaveTextContent('ログアウトしました。')
    expect(screen.queryByText(/99件/)).not.toBeInTheDocument()
  })
})
