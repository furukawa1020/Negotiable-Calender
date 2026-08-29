import { fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import App from './App'

describe('App', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })
  it('explains the privacy projection without exposing sample details as shared data', () => {
    render(<App />)

    expect(screen.getByRole('heading', { name: '今日、どう関われるか。' })).toBeInTheDocument()
    expect(screen.getByText('イベント名・参加者・場所は組織に共有されません')).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '組織に見える状態' })).toBeInTheDocument()
    expect(screen.getByText('15分相談可能')).toBeInTheDocument()
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
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(new Response(JSON.stringify({
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
    fireEvent.click(screen.getByRole('button', { name: '閉じる' }))
    fireEvent.click(screen.getByRole('button', { name: /メンバー表示をプレビュー/ }))
    expect(await screen.findByRole('button', { name: /自分の表示に戻る/ })).toBeInTheDocument()
    expect(globalThis.fetch).toHaveBeenCalledWith(expect.stringContaining('/api/v1/people/demo-manager/projection'))
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
    expect(globalThis.fetch).toHaveBeenCalledWith(expect.stringContaining('/api/v1/people?organizationId=demo-org'))
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
})
