import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { CounterproposalAgreement, CounterproposalForm } from './Counterproposal'
import type { MeetingOffer } from './Counterproposal'

const identity = { apiURL: '', organizationID: 'org', actor: 'bob', requestID: 'r', disabled: false }
const offer: MeetingOffer = { id: 'offer', requestId: 'r', type: 'meeting', startAt: '2099-01-01T01:00:00.000Z', endAt: '2099-01-01T01:30:00.000Z', proposedByUserId: 'bob' }
const json = (body: object, status = 200) => new Response(JSON.stringify(body), { status })
afterEach(() => vi.restoreAllMocks())
const dateInput = (value: string) => { const date = new Date(value); return new Date(date.getTime() - date.getTimezoneOffset() * 60000).toISOString().slice(0, 16) }
function fill() {
  fireEvent.change(screen.getByLabelText('別の開始時間'), { target: { value: dateInput(offer.startAt!) } })
  fireEvent.change(screen.getByLabelText('終了時間'), { target: { value: dateInput(offer.endAt!) } })
}
it('sends an offer with server-owned consent and recovers a lost response using identical times', async () => {
  const fetch = vi.spyOn(globalThis, 'fetch').mockRejectedValueOnce(new Error('lost')).mockResolvedValueOnce(json(offer))
  const done = vi.fn()
  render(<CounterproposalForm {...identity} durationMinutes={30} onProposed={done} />)
  fill(); fireEvent.click(screen.getByRole('button', { name: '別時間を提案' }))
  await screen.findByRole('alert')
  expect(screen.getByLabelText('別の開始時間')).toBeDisabled()
  fireEvent.click(screen.getByRole('button', { name: '別時間を提案' }))
  await waitFor(() => expect(done).toHaveBeenCalledWith(offer))
  expect(fetch.mock.calls[0][1]?.body).toBe(fetch.mock.calls[1][1]?.body)
  expect(JSON.parse(fetch.mock.calls[0][1]?.body as string)).toEqual({ startAt: offer.startAt, endAt: offer.endAt })
  expect(fetch.mock.calls[0][1]).toMatchObject({ credentials: 'include', headers: { 'X-Organization-ID': 'org', 'X-Demo-User-ID': 'bob' } })
})
it('rejects a duration mismatch without calling the server', async () => {
  const fetch = vi.spyOn(globalThis, 'fetch')
  render(<CounterproposalForm {...identity} durationMinutes={15} onProposed={vi.fn()} />)
  fill(); fireEvent.click(screen.getByRole('button', { name: '別時間を提案' }))
  expect(await screen.findByRole('alert')).toHaveTextContent('15分')
  expect(fetch).not.toHaveBeenCalled()
})
it('shows the offer limit without reporting success', async () => {
  vi.spyOn(globalThis, 'fetch').mockResolvedValue(json({ code: 'proposal_limit' }, 409))
  const done = vi.fn()
  render(<CounterproposalForm {...identity} durationMinutes={30} onProposed={done} />)
  fill(); fireEvent.click(screen.getByRole('button', { name: '別時間を提案' }))
  expect(await screen.findByRole('alert')).toHaveTextContent('10件')
  expect(done).not.toHaveBeenCalled()
})
it('does not allow the proposer or requester of generated candidates to self-approve', () => {
  const { rerender } = render(<CounterproposalAgreement {...identity} targetID="bob" status="suggested" options={[offer]} onConfirmed={vi.fn()} />)
  expect(screen.queryByRole('button')).not.toBeInTheDocument()
  rerender(<CounterproposalAgreement {...identity} actor="alice" targetID="bob" status="suggested" options={[{ ...offer, proposedByUserId: undefined }, { ...offer, id: 'old', proposedByUserId: 'former' }]} onConfirmed={vi.fn()} />)
  expect(screen.queryByRole('button')).not.toBeInTheDocument()
})
it('pins an uncertain approval to its original offer, retries, and checks acknowledgement', async () => {
  const fetch = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(json({ status: 'accepted' })).mockResolvedValueOnce(json({ id: 'r', status: 'accepted', acceptedOptionId: 'offer' }))
  const done = vi.fn()
  render(<CounterproposalAgreement {...identity} actor="alice" targetID="bob" status="suggested" options={[offer, { ...offer, id: 'second' }]} onConfirmed={done} />)
  fireEvent.click(screen.getAllByRole('button', { name: 'この提案を承認' })[0])
  expect(await screen.findByRole('alert')).toHaveTextContent('同じ提案への再試行')
  expect(screen.getAllByRole('button', { name: 'この提案を承認' })[1]).toBeDisabled()
  fireEvent.click(screen.getAllByRole('button', { name: 'この提案を承認' })[0])
  await waitFor(() => expect(done).toHaveBeenCalledWith('offer'))
  expect(fetch.mock.calls[0][1]?.body).toBe(fetch.mock.calls[1][1]?.body)
  expect(fetch.mock.calls[0][1]).toMatchObject({ headers: { 'X-Demo-User-ID': 'alice' } })
})
it('keeps stale availability failures unconfirmed', async () => {
  vi.spyOn(globalThis, 'fetch').mockResolvedValue(json({ code: 'availability_changed' }, 409))
  const done = vi.fn()
  render(<CounterproposalAgreement {...identity} actor="alice" targetID="bob" status="suggested" options={[offer]} onConfirmed={done} />)
  fireEvent.click(screen.getByRole('button'))
  expect(await screen.findByRole('alert')).toHaveTextContent('確定できません')
  expect(done).not.toHaveBeenCalled()
})
it('blocks duplicate submission and ignores an acknowledgement after unmount', async () => {
  let settle!: (response: Response) => void
  const fetch = vi.spyOn(globalThis, 'fetch').mockImplementation(() => new Promise(resolve => { settle = resolve }))
  const done = vi.fn()
  const { unmount } = render(<CounterproposalForm {...identity} durationMinutes={30} onProposed={done} />)
  fill(); const form = screen.getByRole('button').closest('form')!
  fireEvent.submit(form); fireEvent.submit(form)
  expect(fetch).toHaveBeenCalledTimes(1)
  unmount()
  expect(fetch.mock.calls[0][1]?.signal?.aborted).toBe(true)
  await act(async () => { settle(json(offer)) })
  expect(done).not.toHaveBeenCalled()
})
