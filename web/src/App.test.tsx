import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import App from './App'

describe('App', () => {
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

  it('opens sharing rules and toggles the member preview', () => {
    render(<App />)

    fireEvent.click(screen.getByRole('button', { name: '共有ルールを確認' }))
    expect(screen.getByRole('dialog', { name: '共有ルール' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '閉じる' }))
    fireEvent.click(screen.getByRole('button', { name: /メンバー表示をプレビュー/ }))
    expect(screen.getByRole('button', { name: /自分の表示に戻る/ })).toBeInTheDocument()
  })
})
