import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { CounterproposalAgreement, CounterproposalForm } from './Counterproposal'
import { RequestHandoff } from './RequestHandoff'

const identity = { apiURL: '', organizationID: 'org', actor: 'bob', requestID: 'r', disabled: false }
const offer = { id: 'offer', requestId: 'r', type: 'meeting' as const, startAt: '2099-01-01T01:00:00.000Z', endAt: '2099-01-01T01:30:00.000Z', proposedByUserId: 'bob' }
const people = { people: [{ id: 'carol', displayName: 'Carol' }] }
const local = (iso: string) => { const date = new Date(iso); return new Date(date.getTime() - date.getTimezoneOffset() * 60000).toISOString().slice(0, 16) }
function deferred<T>() { let resolve!: (v: T) => void; const promise = new Promise<T>(done => { resolve = done }); return { resolve, promise } }
afterEach(() => { cleanup(); vi.useRealTimers(); vi.restoreAllMocks() })

for (const kind of ['proposal', 'accept', 'handoff']) {
  it.each(kind === 'accept' ? ['fetch', 'body'] : ['fetch', 'body', 'error-body'])(`${kind} recovers stalled %s without applying a late result`, async phase => {
    const first = deferred<Response>(), body = deferred<object>(), retry = deferred<Response>()
    const result = kind === 'proposal' ? offer : kind === 'accept' ? { id: 'r', status: 'accepted', acceptedOptionId: 'offer' } : { id: 'r', handedOff: true, delegatedUserId: 'carol' }
    const response = Response.json(result, { status: phase === 'error-body' ? 409 : 200 }); response.json = () => body.promise
    let commands = 0
    const fetch = vi.spyOn(globalThis, 'fetch').mockImplementation(async input => {
      if (String(input).includes('/people')) return Response.json(people)
      commands++
      return commands === 1 ? phase === 'fetch' ? first.promise : response : retry.promise
    })
    const done = vi.fn()
    let button: string
    if (kind === 'proposal') {
      render(<CounterproposalForm {...identity} durationMinutes={30} onProposed={done} />)
      fireEvent.change(screen.getByLabelText('別の開始時間'), { target: { value: local(offer.startAt) } })
      fireEvent.change(screen.getByLabelText('終了時間'), { target: { value: local(offer.endAt) } })
      button = '別時間を提案'
    } else if (kind === 'accept') {
      render(<CounterproposalAgreement {...identity} actor="alice" targetID="bob" status="suggested" options={[offer]} onConfirmed={done} />)
      button = 'この提案を承認'
    } else {
      render(<RequestHandoff {...identity} requesterID="alice" onDone={done} />)
      fireEvent.click(screen.getByRole('button', { name: '担当を引き継ぐ' }))
      await screen.findByRole('option', { name: 'Carol' })
      fireEvent.change(screen.getByLabelText('引継ぎ先'), { target: { value: 'carol' } })
      button = 'この相手へ引き継ぐ'
    }
    vi.useFakeTimers()
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: button })) })
    const firstCall = fetch.mock.calls.at(-1)!
    await act(async () => { await vi.advanceTimersByTimeAsync(20_000) })
    expect(firstCall[1]?.signal?.aborted).toBe(true)
    expect(screen.getByRole('alert')).toHaveTextContent('結果を確認できません')
    expect(done).not.toHaveBeenCalled()
    if (kind === 'proposal') expect(screen.getByLabelText('別の開始時間')).toBeDisabled()
    if (kind === 'handoff') expect(screen.getByLabelText('引継ぎ先')).toBeDisabled()
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: button })) })
    expect(fetch.mock.calls.at(-1)?.[1]?.body).toEqual(firstCall[1]?.body)
    await act(async () => { first.resolve(Response.json(result)); body.resolve(phase === 'error-body' ? { code: 'proposal_limit' } : result) })
    expect(done).not.toHaveBeenCalled()
    expect(commands).toBe(2)
    await act(async () => { retry.resolve(Response.json(result)) })
    expect(done).toHaveBeenCalledTimes(1)
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    expect(vi.getTimerCount()).toBe(0)
  })
}

it('bounds a stalled handoff directory body and discards it after reload', async () => {
  const body = deferred<object>(), response = Response.json({}); response.json = () => body.promise
  vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(response).mockResolvedValueOnce(Response.json(people))
  render(<RequestHandoff {...identity} requesterID="alice" onDone={vi.fn()} />)
  vi.useFakeTimers()
  await act(async () => { fireEvent.click(screen.getByRole('button', { name: '担当を引き継ぐ' })) })
  await act(async () => { await vi.advanceTimersByTimeAsync(20_000) })
  expect(screen.getByRole('alert')).toHaveTextContent('引継ぎ先を取得できません')
  await act(async () => { fireEvent.click(screen.getByRole('button', { name: '引継ぎ先を再読み込み' })) })
  await act(async () => { body.resolve({ people: [{ id: 'old', displayName: 'Old' }] }) })
  expect(screen.getByRole('option', { name: 'Carol' })).toBeInTheDocument()
  expect(screen.queryByRole('option', { name: 'Old' })).not.toBeInTheDocument()
})
