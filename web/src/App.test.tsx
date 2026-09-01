import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import App from './App'

const defaultPolicy = () => ({
  default: {
    availability: 'available', interruptibility: 'normal',
    requestability: 'open', reschedulability: 'medium',
  },
  workingHours: [1, 2, 3, 4, 5].map((weekday) => ({ weekday, startMinute: 540, endMinute: 1080 })),
  rules: [],
})

describe('App', () => {
  afterEach(() => {
    vi.restoreAllMocks()
    window.history.replaceState({}, '', '/')
  })

  it('loads the Google session after callback and logs out', async () => {
    window.history.replaceState({}, '', '/?auth=success')
    vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(new Response(JSON.stringify({
        authenticated: true,
        user: { userId: 'user-1', organizationId: 'org-1', email: 'person@example.com', displayName: 'Person', role: 'OWNER' },
      }), { status: 200 }))
      .mockResolvedValueOnce(new Response(null, { status: 204 }))
    render(<App />)

    expect(await screen.findByRole('status')).toHaveTextContent('Googleアカウントでログインしました。')
    fireEvent.click(screen.getByRole('button', { name: '山田太郎のアカウントメニュー' }))
    expect(screen.getByText('Person')).toBeInTheDocument()
    expect(screen.getByText('OWNER · person@example.com')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'ログアウト' }))

    expect(await screen.findByRole('status')).toHaveTextContent('ログアウトしました。')
    expect(globalThis.fetch).toHaveBeenNthCalledWith(1, expect.stringContaining('/api/v1/auth/session'), { credentials: 'include' })
    expect(globalThis.fetch).toHaveBeenNthCalledWith(2, expect.stringContaining('/api/v1/auth/logout'), { method: 'POST', credentials: 'include' })
  })
  it('explains the privacy projection without exposing sample details as shared data', () => {
    render(<App />)

    expect(screen.getByRole('heading', { name: '今日、どう関われるか。' })).toBeInTheDocument()
    expect(screen.getByText('イベント名・参加者・場所は組織に共有されません')).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '組織に見える状態' })).toBeInTheDocument()
    expect(screen.getByText('15分相談可能')).toBeInTheDocument()
  })

  it('downloads the authenticated self-service data export', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(new Response(JSON.stringify({
      userId: 'demo-manager', requests: [], policy: null, projections: [],
    }), { status: 200, headers: { 'Content-Type': 'application/json' } }))
    const objectURL = vi.spyOn(URL, 'createObjectURL').mockReturnValue('blob:export')
    const revokeObjectURL = vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => undefined)
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => undefined)
    render(<App />)

    fireEvent.click(screen.getByRole('button', { name: '山田太郎のアカウントメニュー' }))
    fireEvent.click(screen.getByRole('button', { name: '本人データをエクスポート' }))

    expect(await screen.findByRole('status')).toHaveTextContent('本人データを安全にエクスポートしました。')
    expect(globalThis.fetch).toHaveBeenCalledWith(
      expect.stringContaining('/api/v1/users/demo-manager/export'),
      expect.objectContaining({ headers: { 'X-Demo-User-ID': 'demo-manager' }, credentials: 'include' }),
    )
    expect(objectURL).toHaveBeenCalled()
    expect(click).toHaveBeenCalled()
    expect(revokeObjectURL).toHaveBeenCalledWith('blob:export')
  })

  it('creates a coordination request from the dialog', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(new Response(JSON.stringify({
      status: 'suggested',
      options: [{ type: 'meeting' }, { type: 'async' }],
    }), { status: 201 }))
    render(<App />)

    fireEvent.click(screen.getByRole('button', { name: '依頼を作成' }))
    expect(screen.getByRole('dialog', { name: '依頼を作成' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '候補を生成して送信' }))
    expect(await screen.findByRole('status')).toHaveTextContent('2件の候補を生成しました')
    expect(globalThis.fetch).toHaveBeenCalledWith(
      expect.stringContaining('/api/v1/requests'),
      expect.objectContaining({ method: 'POST' }),
    )
  })

  it('opens sharing rules and loads the member preview from the public API', async () => {
    vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(new Response(JSON.stringify(defaultPolicy()), { status: 200 }))
      .mockResolvedValueOnce(new Response(JSON.stringify({
      segments: [{
        startAt: '2026-08-23T00:00:00Z',
        endAt: '2026-08-23T01:00:00Z',
        availability: 'available',
        interruptibility: 'open',
      }],
      }), { status: 200, headers: { 'Content-Type': 'application/json' } }))
    render(<App />)

    fireEvent.click(screen.getByRole('button', { name: '共有ルールを確認' }))
    expect(screen.getByRole('dialog', { name: '共有ルール' })).toBeInTheDocument()
    await waitFor(() => expect(screen.getByLabelText('基本の公開状態')).not.toBeDisabled())
    fireEvent.click(screen.getByRole('button', { name: '閉じる' }))
    fireEvent.click(screen.getByRole('button', { name: /メンバー表示をプレビュー/ }))
    expect(await screen.findByRole('button', { name: /自分の表示に戻る/ })).toBeInTheDocument()
    expect(globalThis.fetch).toHaveBeenCalledWith(
      expect.stringContaining('/api/v1/people/demo-manager/projection'),
      expect.objectContaining({ headers: expect.objectContaining({ 'X-Organization-ID': 'demo-org' }) }),
    )
  })

  it('loads and persists sharing policy changes through the API', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(new Response(JSON.stringify(defaultPolicy()), { status: 200 }))
      .mockResolvedValueOnce(new Response(JSON.stringify({
        ...defaultPolicy(), default: { ...defaultPolicy().default, availability: 'limited' },
      }), { status: 200 }))
    render(<App />)

    fireEvent.click(screen.getByRole('button', { name: '共有ルールを確認' }))
    const availability = screen.getByLabelText('基本の公開状態')
    await waitFor(() => expect(availability).not.toBeDisabled())
    fireEvent.change(availability, { target: { value: 'limited' } })
    fireEvent.click(screen.getByRole('button', { name: 'このルールを保存' }))

    expect(await screen.findByRole('status')).toHaveTextContent('共有ルールを保存しました。')
    const [, options] = fetchMock.mock.calls[1]
    expect(options).toEqual(expect.objectContaining({
      method: 'PUT',
      headers: expect.objectContaining({ 'X-Demo-User-ID': 'demo-manager' }),
    }))
    expect(JSON.parse(String(options?.body))).toEqual(expect.objectContaining({
      default: expect.objectContaining({ availability: 'limited' }),
      workingHours: expect.arrayContaining([expect.objectContaining({ weekday: 1, startMinute: 540, endMinute: 1080 })]),
    }))
  })

  it('navigates days and switches manager calendar layers', () => {
    render(<App />)
    const initialDate = screen.getByText(/月|火|水|木|金|土|日/, { selector: '.eyebrow' }).textContent
    fireEvent.click(screen.getByRole('button', { name: '次の日' }))
    expect(screen.getByText(/月|火|水|木|金|土|日/, { selector: '.eyebrow' }).textContent).not.toBe(initialDate)
    fireEvent.click(screen.getByRole('button', { name: 'Private' }))
    expect(screen.getByLabelText('今日のプライベート予定と公開状態')).toHaveClass('layer-private')
  })

  it('persists a manual override and reflects it in the projection', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response('{}', { status: 201 }))
    render(<App />)
    fireEvent.click(screen.getByRole('button', { name: '状態を上書き' }))
    expect(screen.getByRole('dialog', { name: '公開状態を上書き' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '上書きを保存' }))
    expect(await screen.findByText('公開状態を上書きしました。')).toBeInTheDocument()
    expect(screen.getByText('相談可能（上書き）')).toBeInTheDocument()
    expect(globalThis.fetch).toHaveBeenCalledWith(
      expect.stringContaining('/manual-overrides'),
      expect.objectContaining({ method: 'POST' }),
    )
  })

  it('loads the organization people view from privacy-safe APIs', async () => {
    vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(new Response(JSON.stringify({
        people: [{ id: 'demo-manager', displayName: '山田 太郎', timezone: 'Asia/Tokyo', role: 'MANAGER' }],
      }), { status: 200 }))
      .mockResolvedValueOnce(new Response(JSON.stringify({
        segments: [{
          startAt: '2026-08-23T00:00:00Z',
          endAt: '2026-08-23T01:00:00Z',
          availability: 'limited',
          interruptibility: 'urgent_only',
        }],
      }), { status: 200 }))
    render(<App />)
    fireEvent.click(screen.getByRole('button', { name: '組織' }))
    expect(await screen.findByRole('heading', { name: '誰に、どう相談できるか。' })).toBeInTheDocument()
    expect(await screen.findByRole('heading', { name: '山田 太郎' })).toBeInTheDocument()
    expect(screen.getByText('緊急のみ')).toBeInTheDocument()
    expect(screen.queryByText('Product Review')).not.toBeInTheDocument()
    expect(globalThis.fetch).toHaveBeenCalledWith(
      expect.stringContaining('/api/v1/people?organizationId=demo-org'),
      expect.objectContaining({ headers: expect.objectContaining({ 'X-Demo-User-ID': 'demo-manager' }) }),
    )
  })

  it('loads the manager request inbox with generated options', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(new Response(JSON.stringify({
      requests: [{
        id: 'request-1', requesterUserId: 'demo-member', title: '新API設計レビュー',
        type: 'review', durationMinutes: 15, deadlineAt: '2026-08-27T08:00:00Z',
        priority: 'high', status: 'suggested', createdAt: '2026-08-27T00:00:00Z',
        options: [{
          id: 'option-1', type: 'meeting',
          startAt: '2026-08-27T01:00:00Z', endAt: '2026-08-27T01:15:00Z',
        }],
      }],
    }), { status: 200 })).mockResolvedValueOnce(new Response(JSON.stringify({
      id: 'option-2', type: 'meeting',
      startAt: '2026-08-28T01:00:00Z', endAt: '2026-08-28T01:15:00Z',
    }), { status: 201 })).mockResolvedValue(new Response('{}', { status: 200 }))
    render(<App />)
    fireEvent.click(screen.getByRole('button', { name: '依頼' }))
    expect(await screen.findByRole('heading', { name: '届いた依頼を、余白から選ぶ。' })).toBeInTheDocument()
    expect(await screen.findByRole('heading', { name: '新API設計レビュー' })).toBeInTheDocument()
    expect(screen.getByText('MEETING')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '委譲する' })).toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('別の開始時間'), { target: { value: '2026-08-28T10:00' } })
    fireEvent.change(screen.getByLabelText('終了時間'), { target: { value: '2026-08-28T10:15' } })
    fireEvent.click(screen.getByRole('button', { name: '別時間を提案' }))
    expect(await screen.findByText('別の時間候補を追加しました。')).toBeInTheDocument()
    fireEvent.click(screen.getAllByRole('button', { name: 'この候補を承認' })[0])
    expect(await screen.findByText('候補を承認しました。')).toBeInTheDocument()
    expect(screen.queryByText('Product Review')).not.toBeInTheDocument()
    expect(globalThis.fetch).toHaveBeenCalledWith(
      expect.stringContaining('/api/v1/requests'),
      expect.objectContaining({ headers: { 'X-Demo-User-ID': 'demo-manager' } }),
    )
    expect(globalThis.fetch).toHaveBeenCalledWith(
      expect.stringContaining('/api/v1/requests/request-1/suggest'),
      expect.objectContaining({ method: 'POST' }),
    )
    expect(globalThis.fetch).toHaveBeenCalledWith(
      expect.stringContaining('/api/v1/requests/request-1/accept'),
      expect.objectContaining({ method: 'POST' }),
    )
  })

  it('loads sent requests and lets the requester cancel an active request', async () => {
    vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(new Response(JSON.stringify({
        requests: [{
          id: 'request-1', requesterUserId: 'demo-member', targetUserId: 'demo-manager',
          title: '新API設計レビュー', type: 'review', durationMinutes: 15,
          deadlineAt: '2026-09-02T08:00:00Z', priority: 'normal', status: 'suggested',
          createdAt: '2026-09-01T00:00:00Z', options: [{ id: 'option-1', type: 'meeting' }],
        }],
      }), { status: 200 }))
      .mockResolvedValueOnce(new Response(JSON.stringify({ id: 'request-1', status: 'cancelled' }), { status: 200 }))
    render(<App />)

    fireEvent.click(screen.getByRole('button', { name: '送信済み' }))
    expect(await screen.findByRole('heading', { name: '新API設計レビュー' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '依頼をキャンセル' }))

    expect(await screen.findByRole('status')).toHaveTextContent('依頼をキャンセルしました。相手にも通知しました。')
    expect(screen.getByText('更新済み · cancelled')).toBeInTheDocument()
    expect(globalThis.fetch).toHaveBeenNthCalledWith(1,
      expect.stringContaining('/api/v1/requests?scope=sent'),
      expect.objectContaining({ headers: { 'X-Demo-User-ID': 'demo-member' }, credentials: 'include' }),
    )
    expect(globalThis.fetch).toHaveBeenNthCalledWith(2,
      expect.stringContaining('/api/v1/requests/request-1/cancel'),
      expect.objectContaining({ method: 'POST', headers: { 'X-Demo-User-ID': 'demo-member' }, credentials: 'include' }),
    )
  })

  it('delegates an inbox request to an organization member', async () => {
    vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(new Response(JSON.stringify({
        requests: [{
          id: 'request-2', requesterUserId: 'requester-1', title: '承認フロー確認',
          type: 'approval', durationMinutes: 5, deadlineAt: '2026-08-29T08:00:00Z',
          priority: 'normal', status: 'suggested', createdAt: '2026-08-28T00:00:00Z',
          options: [{ id: 'option-1', type: 'async' }],
        }],
      }), { status: 200 }))
      .mockResolvedValueOnce(new Response(JSON.stringify({ status: 'delegated' }), { status: 200 }))
    render(<App />)
    fireEvent.click(screen.getByRole('button', { name: '依頼' }))
    expect(await screen.findByRole('heading', { name: '承認フロー確認' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '委譲する' }))
    expect(await screen.findByText('demo-member に依頼を委譲しました。')).toBeInTheDocument()
    expect(screen.getByText('回答済み · delegated')).toBeInTheDocument()
    expect(globalThis.fetch).toHaveBeenCalledWith(
      expect.stringContaining('/api/v1/requests/request-2/delegate'),
      expect.objectContaining({ method: 'POST' }),
    )
  })

  it('loads privacy-safe notifications and marks one read', async () => {
    vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(new Response(JSON.stringify({
        notifications: [{
          id: 'notification-1', type: 'request_received', requestId: 'request-1',
          message: '新しい調整依頼が届きました。', createdAt: '2026-08-30T01:00:00Z',
        }],
      }), { status: 200 }))
      .mockResolvedValueOnce(new Response(null, { status: 204 }))
    render(<App />)
    fireEvent.click(screen.getByRole('button', { name: '通知' }))
    const panel = await screen.findByLabelText('通知一覧')
    expect(panel).toBeInTheDocument()
    expect(await screen.findByText('新しい調整依頼が届きました。')).toBeInTheDocument()
    expect(within(panel).queryByText('Product Review')).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: /新しい調整依頼が届きました/ }))
    expect(globalThis.fetch).toHaveBeenCalledWith(
      expect.stringContaining('/api/v1/notifications/notification-1/read'),
      expect.objectContaining({ method: 'POST' }),
    )
  })

  it('shows a privacy-safe organization audit log', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify({
      auditLogs: [{
        id: 'audit-1', organizationId: 'demo-org', actorUserId: 'demo-manager',
        action: 'request_accepted', resourceType: 'request', resourceId: 'request-1',
        createdAt: '2026-08-30T02:00:00Z',
      }],
    }), { status: 200 }))
    render(<App />)
    fireEvent.click(screen.getByRole('button', { name: '監査' }))
    const heading = await screen.findByRole('heading', { name: '共有した事実だけを、記録する。' })
    expect(heading).toBeInTheDocument()
    expect(await screen.findByText('request accepted')).toBeInTheDocument()
    expect(screen.getByText('予定詳細なし')).toBeInTheDocument()
    const auditView = heading.closest('.audit-view') as HTMLElement
    expect(within(auditView).queryByText('Product Review')).not.toBeInTheDocument()
    expect(globalThis.fetch).toHaveBeenCalledWith(
      expect.stringContaining('/api/v1/audit-logs'),
      expect.objectContaining({ headers: expect.objectContaining({ 'X-Organization-ID': 'demo-org' }) }),
    )
  })
})
