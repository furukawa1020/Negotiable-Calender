import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { ConfirmedMeeting } from './ConfirmedMeeting'

const request = { id: 'r', status: 'accepted', acceptedOptionId: 'chosen', options: [
  { id: 'other', type: 'meeting', startAt: '2026-09-20T01:00:00Z', endAt: '2026-09-20T02:00:00Z' },
  { id: 'chosen', type: 'meeting', startAt: '2026-09-21T01:00:00Z', endAt: '2026-09-21T02:00:00Z' },
] }

describe('ConfirmedMeeting', () => {
  it('shows only the chosen time and downloads on explicit action', async () => {
    const download = vi.fn().mockResolvedValue(undefined)
    const { container } = render(<ConfirmedMeeting request={request} onDownload={download} />)
    expect(container.querySelector('time')?.dateTime).toBe(request.options[1].startAt)
    expect(download).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: 'カレンダーに登録（ICS）' }))
    await waitFor(() => expect(download).toHaveBeenCalledWith('r'))
  })
  it('does not export pending, async or missing selections', () => {
    const { rerender } = render(<ConfirmedMeeting request={{ ...request, status: 'suggested' }} onDownload={vi.fn()} />)
    expect(screen.queryByRole('button')).not.toBeInTheDocument()
    rerender(<ConfirmedMeeting request={{ ...request, acceptedOptionId: 'missing' }} onDownload={vi.fn()} />)
    expect(screen.queryByRole('button')).not.toBeInTheDocument()
    rerender(<ConfirmedMeeting request={{ ...request, options: [{ id: 'chosen', type: 'async' }] }} onDownload={vi.fn()} />)
    expect(screen.queryByRole('button')).not.toBeInTheDocument()
  })
  it('reports failure and permits retry', async () => {
    const download = vi.fn().mockRejectedValueOnce(new Error('conflict')).mockResolvedValue(undefined)
    render(<ConfirmedMeeting request={request} onDownload={download} />)
    fireEvent.click(screen.getByRole('button'))
    expect(await screen.findByRole('alert')).toHaveTextContent('再試行')
    fireEvent.click(screen.getByRole('button'))
    await waitFor(() => expect(screen.queryByRole('alert')).not.toBeInTheDocument())
    expect(download).toHaveBeenCalledTimes(2)
  })
})
