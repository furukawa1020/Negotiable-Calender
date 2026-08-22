import { render, screen } from '@testing-library/react'
import App from './App'

describe('App', () => {
  it('explains the privacy projection without exposing sample details as shared data', () => {
    render(<App />)

    expect(screen.getByRole('heading', { name: '今日、どう関われるか。' })).toBeInTheDocument()
    expect(screen.getByText('イベント名・参加者・場所は組織に共有されません')).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '組織に見える状態' })).toBeInTheDocument()
    expect(screen.getByText('15分相談可能')).toBeInTheDocument()
  })
})
