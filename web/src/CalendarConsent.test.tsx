import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { CalendarConsent, CalendarConnectionHelp } from './CalendarConsent'
import { calendarConsentNotice } from './calendarConsentNotice'

describe('Calendar consent', () => {
  it('explains actual access before offering optional read-only connection', () => {
    render(<CalendarConsent connectURL="/api/v1/calendar/google/connect" />)
    expect(screen.getByRole('region', { name: 'カレンダー接続前の確認' })).toBeInTheDocument()
    expect(screen.getByText(/Google上の予定は作成・変更しません/)).toBeInTheDocument()
    expect(screen.getByText(/自分が所有するカレンダーの予定の読み取り/)).toBeInTheDocument()
    expect(screen.getByText(/認証情報は暗号化して保管/)).toBeInTheDocument()
    expect(screen.getByText(/カレンダー接続は任意/)).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Google Calendarを接続' })).toHaveAttribute('href', '/api/v1/calendar/google/connect')
  })

  it('keeps reconnect explicit', () => {
    render(<CalendarConsent connectURL="/connect" reconnect />)
    expect(screen.getByRole('link', { name: 'Google Calendarを再接続' })).toHaveAttribute('href', '/connect')
  })

  it('helps with provider-hosted denial without asking for secrets or bypassing restrictions', () => {
    render(<CalendarConnectionHelp />)
    expect(screen.getByText(/テストユーザー登録や警告の回避は解決手順ではありません/)).toBeInTheDocument()
    expect(screen.getByText(/認証コード、Cookie、秘密鍵は送らない/)).toBeInTheDocument()
  })

  it.each(['denied', 'permission_required', 'exchange_failed', 'provider_failed'])('maps %s to a fixed non-destructive notice', (result) => {
    expect(calendarConsentNotice(result)).toContain('既存の接続は変更していません')
  })

  it.each([null, 'connected', '<script>secret</script>', 'access_denied&code=secret'])('does not reflect unknown result %s', (result) => {
    expect(calendarConsentNotice(result)).toBe('')
  })
})
