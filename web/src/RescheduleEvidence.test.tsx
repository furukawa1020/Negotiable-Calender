import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { RescheduleMeeting } from './RescheduleMeeting'

const base = Date.parse('2030-01-01T00:00:00Z')
const at = (offset: number) => new Date(base + offset).toISOString()
function fixture() { return { id: 'r', status: 'accepted', acceptedOptionId: 'old', durationMinutes: 30, deadlineAt: at(10000), rescheduleProposal: { id: 'new', expectedOptionId: 'old', proposerUserId: 'alice', status: 'proposed' }, options: [
  { id: 'old', type: 'meeting', startAt: at(2000), endAt: at(4000) },
  { id: 'new', type: 'meeting', startAt: at(1000), endAt: at(3000) },
] } }
beforeEach(() => { vi.useFakeTimers(); vi.setSystemTime(base) })
afterEach(() => { cleanup(); vi.useRealTimers() })

it.each(['duplicate-selected', 'duplicate-proposal', 'wrong-selection', 'bad-time', 'wrong-duration', 'wrong-type'])('never submits ambiguous %s proposal evidence', scenario => {
  const value = fixture(), change = vi.fn()
  if (scenario === 'duplicate-selected') value.options.push(value.options[0])
  if (scenario === 'duplicate-proposal') value.options.push(value.options[1])
  if (scenario === 'wrong-selection') value.rescheduleProposal.expectedOptionId = 'other'
  if (scenario === 'bad-time') value.options[1].startAt = 'invalid'
  if (scenario === 'wrong-duration') value.options[1].endAt = at(3500)
  if (scenario === 'wrong-type') value.options[1].type = 'async'
  render(<RescheduleMeeting request={value} actor="bob" onChange={change} />)
  expect(screen.queryByRole('button', { name: 'この日時への変更を承認' })).not.toBeInTheDocument()
  expect(change).not.toHaveBeenCalled()
})

it('disables expired proposal approval, retains decline, then removes controls at original start', () => {
  const change = vi.fn(), view = render(<RescheduleMeeting request={fixture()} actor="bob" onChange={change} />)
  act(() => { vi.advanceTimersByTime(1000) })
  expect(screen.getByRole('button', { name: 'この日時への変更を承認' })).toBeDisabled()
  expect(screen.getByRole('button', { name: '変更提案を辞退' })).toBeEnabled()
  act(() => { vi.advanceTimersByTime(1000) })
  expect(screen.queryByRole('button')).not.toBeInTheDocument()
  expect(change).not.toHaveBeenCalled()
  view.unmount(); expect(vi.getTimerCount()).toBe(0)
})

it.each(['focus', 'visibilitychange'])('reconciles %s and rechecks wall time before timer delivery', event => {
  const change = vi.fn()
  render(<RescheduleMeeting request={fixture()} actor="bob" onChange={change} />)
  vi.setSystemTime(base + 2000)
  fireEvent.click(screen.getByRole('button', { name: 'この日時への変更を承認' }))
  expect(change).not.toHaveBeenCalled()
  act(() => { (event === 'focus' ? window : document).dispatchEvent(new Event(event)) })
  expect(screen.queryByRole('button')).not.toBeInTheDocument()
})

it('resets edited form and ignores late failure after changed request evidence', async () => {
  let reject!: (error: Error) => void
  const pending = new Promise<void>((_, fail) => { reject = fail })
  const value = { ...fixture(), rescheduleProposal: undefined }
  const view = render(<RescheduleMeeting request={value} actor="alice" onChange={() => pending} />)
  fireEvent.click(screen.getByRole('button', { name: '日時変更を提案' }))
  fireEvent.change(screen.getByLabelText('変更後の開始日時'), { target: { value: '2030-01-02T12:00' } })
  fireEvent.click(screen.getByRole('button', { name: '日時変更の提案を送信' }))
  view.rerender(<RescheduleMeeting request={{ ...value, id: 'replacement' }} actor="alice" onChange={vi.fn()} />)
  await act(async () => { reject(new Error('old failure')); await pending.catch(() => undefined) })
  expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  expect(screen.getByRole('button', { name: '日時変更を提案' })).toBeEnabled()
})
