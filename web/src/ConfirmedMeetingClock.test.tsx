import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ConfirmedMeeting } from './ConfirmedMeeting'

const base = Date.parse('2030-01-01T00:00:00Z')
function meeting(start = base + 1000, end = base + 2000) {
  return { id: 'r', status: 'accepted', acceptedOptionId: 'a', options: [{ id: 'a', type: 'meeting', startAt: new Date(start).toISOString(), endAt: new Date(end).toISOString() }] }
}
beforeEach(() => { vi.useFakeTimers(); vi.setSystemTime(base) })
afterEach(() => { cleanup(); vi.useRealTimers() })

describe('confirmed meeting lifecycle clock', () => {
  it('crosses both boundaries without reload or commands and disposes its timer', () => {
    const cancel = vi.fn(), complete = vi.fn()
    const view = render(<ConfirmedMeeting request={meeting()} onDownload={vi.fn()} onCancel={cancel} onComplete={complete} />)
    fireEvent.click(screen.getByRole('button', { name: '確定会議を取り消す' }))
    act(() => { vi.advanceTimersByTime(1000) })
    expect(screen.queryByRole('button', { name: '会議の取消を確定する' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '会議を完了にする' })).not.toBeInTheDocument()
    act(() => { vi.advanceTimersByTime(1000) })
    expect(screen.getByRole('button', { name: '会議を完了にする' })).toBeEnabled()
    expect(cancel).not.toHaveBeenCalled()
    expect(complete).not.toHaveBeenCalled()
    expect(vi.getTimerCount()).toBe(0)
    view.rerender(<ConfirmedMeeting request={meeting(base + 5000, base + 6000)} onDownload={vi.fn()} onCancel={cancel} />)
    expect(vi.getTimerCount()).toBe(1)
    view.unmount()
    expect(vi.getTimerCount()).toBe(0)
  })

  it.each(['focus', 'visibilitychange'])('reconciles sleeping tabs on %s', event => {
    render(<ConfirmedMeeting request={meeting()} onDownload={vi.fn()} onCancel={vi.fn()} onComplete={vi.fn()} />)
    vi.setSystemTime(base + 3000)
    act(() => { (event === 'focus' ? window : document).dispatchEvent(new Event(event)) })
    expect(screen.queryByRole('button', { name: '確定会議を取り消す' })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: '会議を完了にする' })).toBeEnabled()
    expect(vi.getTimerCount()).toBe(0)
  })

  it('clamps far future timers and refuses stale confirmation even before timer delivery', () => {
    const start = base + 90 * 86400000
    const cancel = vi.fn()
    render(<ConfirmedMeeting request={meeting(start, start + 1000)} onDownload={vi.fn()} onCancel={cancel} />)
    act(() => { vi.advanceTimersByTime(2_147_483_647) })
    expect(vi.getTimerCount()).toBe(1)
    fireEvent.click(screen.getByRole('button', { name: '確定会議を取り消す' }))
    vi.setSystemTime(start)
    fireEvent.click(screen.getByRole('button', { name: '会議の取消を確定する' }))
    expect(cancel).not.toHaveBeenCalled()
    expect(screen.getByRole('alert')).toHaveTextContent('開始時刻')
  })

  it.each(['request', 'option', 'time', 'proposal'])('requires a new confirmation after changed %s evidence', change => {
    const cancel = vi.fn()
    const value = meeting()
    const view = render(<ConfirmedMeeting request={value} onDownload={vi.fn()} onCancel={cancel} />)
    fireEvent.click(screen.getByRole('button', { name: '確定会議を取り消す' }))
    const next = { ...value, options: value.options.map(option => ({ ...option })) }
    if (change === 'request') next.id = 'other'
    if (change === 'option') { next.acceptedOptionId = 'b'; next.options[0].id = 'b' }
    if (change === 'time') next.options[0].endAt = new Date(base + 5000).toISOString()
    const request = change === 'proposal' ? { ...next, rescheduleProposal: { status: 'proposed' } } : next
    view.rerender(<ConfirmedMeeting request={request} onDownload={vi.fn()} onCancel={cancel} />)
    expect(screen.queryByRole('button', { name: '会議の取消を確定する' })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: '確定会議を取り消す' })).toBeEnabled()
    expect(cancel).not.toHaveBeenCalled()
  })

  it('does not apply late local errors to replacement evidence', async () => {
    let reject!: (error: Error) => void
    const pending = new Promise<void>((_, fail) => { reject = fail })
    const view = render(<ConfirmedMeeting request={meeting()} onDownload={() => pending} />)
    fireEvent.click(screen.getByRole('button', { name: 'カレンダーに登録（ICS）' }))
    view.rerender(<ConfirmedMeeting request={{ ...meeting(), id: 'new' }} onDownload={vi.fn()} />)
    await act(async () => { reject(new Error('old failure')); await pending.catch(() => undefined) })
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'カレンダーに登録（ICS）' })).toBeEnabled()
  })
})
