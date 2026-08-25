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

  it('creates a coordination request from the dialog', () => {
    render(<App />)

    fireEvent.click(screen.getByRole('button', { name: '依頼を作成' }))
    expect(screen.getByRole('dialog', { name: '依頼を作成' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '候補を生成して送信' }))
    expect(screen.getByRole('status')).toHaveTextContent('レビュー依頼を送信しました')
  })

  it('opens sharing rules and loads the member preview from the public API', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify({
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
})
