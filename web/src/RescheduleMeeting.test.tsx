import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { RescheduleMeeting } from './RescheduleMeeting'

const at = (hours: number) => new Date(Date.now() + hours * 3600000).toISOString()
const fixture = () => ({ id: 'r', status: 'accepted', acceptedOptionId: 'original', durationMinutes: 30, deadlineAt: at(24), options: [{ id: 'original', type: 'meeting', startAt: at(1), endAt: at(1.5) }, { id: 'proposal-new', type: 'meeting', startAt: at(2), endAt: at(2.5) }] })
const proposal = { id: 'proposal-new', proposerUserId: 'alice', expectedOptionId: 'original', status: 'proposed' }

describe('RescheduleMeeting', () => {
  it('proposes explicitly and retries the same payload with the same id', async () => {
    const onChange = vi.fn().mockRejectedValueOnce(new Error('network')).mockResolvedValue(undefined)
    render(<RescheduleMeeting request={fixture()} actor="alice" onChange={onChange} />)
    fireEvent.click(screen.getByRole('button', { name: '日時変更を提案' }))
    fireEvent.change(screen.getByLabelText('変更後の開始日時'), { target: { value: '2099-01-01T12:00' } })
    expect(onChange).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: '日時変更の提案を送信' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('確定日時')
    fireEvent.click(screen.getByRole('button', { name: '日時変更の提案を送信' }))
    await waitFor(() => expect(screen.queryByRole('alert')).not.toBeInTheDocument())
    expect(onChange).toHaveBeenCalledTimes(2)
    expect(onChange.mock.calls[0]).toEqual(onChange.mock.calls[1])
    expect(onChange.mock.calls[0][1]).toMatchObject({ action: 'propose', expectedOptionId: 'original', startAt: new Date('2099-01-01T12:00').toISOString() })
  })
  it('only offers withdrawal to the proposer and approval/decline to the counterpart', async () => {
    const onChange = vi.fn().mockResolvedValue(undefined)
    const value = { ...fixture(), rescheduleProposal: proposal }
    const { rerender } = render(<RescheduleMeeting request={value} actor="alice" onChange={onChange} />)
    expect(screen.queryByRole('button', { name: 'この日時への変更を承認' })).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '変更提案を撤回' }))
    await waitFor(() => expect(onChange).toHaveBeenCalledWith('r', { action: 'withdraw', proposalId: 'proposal-new', expectedOptionId: 'original' }))
    rerender(<RescheduleMeeting request={value} actor="bob" onChange={onChange} />)
    expect(screen.queryByRole('button', { name: '変更提案を撤回' })).not.toBeInTheDocument()
    expect(screen.getByText(/自動更新されません/)).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'この日時への変更を承認' }))
    await waitFor(() => expect(onChange).toHaveBeenLastCalledWith('r', { action: 'accept', proposalId: 'proposal-new', expectedOptionId: 'original' }))
  })
  it('keeps failed approval retryable and allows decline', async () => {
    const onChange = vi.fn().mockRejectedValueOnce(new Error('conflict')).mockResolvedValue(undefined)
    render(<RescheduleMeeting request={{ ...fixture(), rescheduleProposal: proposal }} actor="bob" onChange={onChange} />)
    fireEvent.click(screen.getByRole('button', { name: 'この日時への変更を承認' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('競合')
    fireEvent.click(screen.getByRole('button', { name: '変更提案を辞退' }))
    await waitFor(() => expect(onChange).toHaveBeenLastCalledWith('r', { action: 'decline', proposalId: 'proposal-new', expectedOptionId: 'original' }))
  })
  it('hides mutation controls for cancelled and already started meetings', () => {
    const value = fixture()
    const { rerender } = render(<RescheduleMeeting request={{ ...value, status: 'cancelled' }} actor="alice" onChange={vi.fn()} />)
    expect(screen.queryByRole('button')).not.toBeInTheDocument()
    rerender(<RescheduleMeeting request={{ ...value, options: [{ id: 'original', type: 'meeting', startAt: at(-1), endAt: at(-0.5) }] }} actor="alice" onChange={vi.fn()} />)
    expect(screen.queryByRole('button')).not.toBeInTheDocument()
  })
})
