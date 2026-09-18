import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { CalendarSyncStatus } from './CalendarSyncStatus'

describe('CalendarSyncStatus', () => {
  it('warns when the server cannot verify a recently timestamped source', () => {
    render(<CalendarSyncStatus mode="external" connection={{ reconnectRequired: false, sourceFresh: false, lastSyncedAt: new Date().toISOString() }} />)
    expect(screen.getByText(/鮮度を確認できません/)).toBeInTheDocument()
  })
  it('distinguishes disabled automation from a fresh sync', () => {
    render(<CalendarSyncStatus mode="off" connection={{ reconnectRequired: false, lastSyncedAt: new Date().toISOString() }} />)
    expect(screen.getByText(/自動同期は停止中/)).toBeInTheDocument()
    expect(screen.queryByText(/鮮度を確認できません/)).not.toBeInTheDocument()
  })
  it('warns about missing/stale sync and avoids claiming an exact next run', () => {
    const { rerender } = render(<CalendarSyncStatus mode="external" connection={{ reconnectRequired: false, nextAttemptAt: new Date().toISOString() }} />)
    expect(screen.getByRole('status')).toHaveTextContent('鮮度を確認できません')
    expect(screen.getByText(/遅延する場合/)).toBeInTheDocument()
    rerender(<CalendarSyncStatus mode="external" connection={{ reconnectRequired: false, lastSyncedAt: '2020-01-01T00:00:00Z', lastErrorCode: 'timeout' }} />)
    expect(screen.getByText(/前回の同期に失敗/)).toBeInTheDocument()
    expect(screen.getByText(/鮮度を確認できません/)).toBeInTheDocument()
  })
  it('requires reconnect instead of promising retries for a revoked grant', () => {
    render(<CalendarSyncStatus mode="external" connection={{ reconnectRequired: true, lastErrorCode: 'reconnect_required' }} />)
    expect(screen.getByText('Calendarの再接続が必要です')).toBeInTheDocument()
    expect(screen.queryByText(/次の起動/)).not.toBeInTheDocument()
  })
})
