import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { RequestComposer } from './RequestComposer'

const props = { apiURL: '', organizationID: 'org', requesterID: 'alice', initialTargetID: 'bob', demo: false, onClose: vi.fn(), onCreated: vi.fn() }
const people = { people: [{ id: 'bob', displayName: 'Bob' }] }
const created = { id: 'r', organizationId: 'org', requesterUserId: 'alice', targetUserId: 'bob', status: 'suggested', options: [] }
function deferred<T>() { let resolve!: (v: T) => void; const promise = new Promise<T>(done => { resolve = done }); return { resolve, promise } }
afterEach(() => { cleanup(); vi.useRealTimers(); vi.restoreAllMocks(); props.onCreated.mockClear() })
async function fill() {
  await waitFor(() => expect(screen.getByLabelText('依頼先')).toBeEnabled())
  fireEvent.change(screen.getByLabelText('依頼内容'), { target: { value: 'Synthetic request' } })
}

it.each(['fetch', 'body', 'error-body'])('settles stalled creation %s, retains retry key and ignores late success', async phase => {
  const pending = deferred<Response>(), body = deferred<object>()
  const response = Response.json(created, { status: phase === 'error-body' ? 409 : 201 }); response.json = () => body.promise
  const fetch = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(Response.json(people)).mockImplementationOnce(() => phase === 'fetch' ? pending.promise : Promise.resolve(response)).mockResolvedValueOnce(Response.json(created))
  render(<RequestComposer {...props} />); await fill(); vi.useFakeTimers()
  await act(async () => { fireEvent.click(screen.getByRole('button', { name: '候補を生成して送信' })) })
  await act(async () => { await vi.advanceTimersByTimeAsync(20_000) })
  expect(fetch.mock.calls[1][1]?.signal?.aborted).toBe(true)
  expect(screen.getByRole('alert')).toHaveTextContent('同じ内容で再送')
  expect(props.onCreated).not.toHaveBeenCalled()
  await act(async () => { fireEvent.click(screen.getByRole('button', { name: '候補を生成して送信' })) })
  expect(props.onCreated).toHaveBeenCalledTimes(1)
  expect(fetch.mock.calls[1][1]?.body).toEqual(fetch.mock.calls[2][1]?.body)
  expect(fetch.mock.calls[1][1]?.headers).toEqual(fetch.mock.calls[2][1]?.headers)
  await act(async () => { pending.resolve(Response.json(created)); body.resolve(created) })
  expect(props.onCreated).toHaveBeenCalledTimes(1)
  expect(vi.getTimerCount()).toBe(0)
})

it('bounds recipient body reads and allows a clean reload', async () => {
  const body = deferred<object>(), response = Response.json({}); response.json = () => body.promise
  vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(response).mockResolvedValueOnce(Response.json(people))
  vi.useFakeTimers(); render(<RequestComposer {...props} />)
  await act(async () => { await vi.advanceTimersByTimeAsync(20_000) })
  expect(screen.getByRole('alert')).toHaveTextContent('依頼先を取得できません')
  await act(async () => { fireEvent.click(screen.getByRole('button', { name: '依頼先を再読み込み' })) })
  await act(async () => { body.resolve({ people: [{ id: 'old', displayName: 'Old recipient' }] }) })
  expect(screen.getByRole('option', { name: 'Bob' })).toBeInTheDocument()
  expect(screen.queryByText('Old recipient')).not.toBeInTheDocument()
})

it.each([null, {}, { ...created, organizationId: 'other' }, { ...created, requesterUserId: 'other' }, { ...created, targetUserId: 'other' }, { ...created, status: 'unknown' }, { ...created, options: null }])('does not report malformed or foreign acknowledgement %j as created', async value => {
  vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(Response.json(people)).mockResolvedValueOnce(Response.json(value))
  render(<RequestComposer {...props} />); await fill()
  fireEvent.click(screen.getByRole('button', { name: '候補を生成して送信' }))
  expect(await screen.findByRole('alert')).toHaveTextContent('送信結果を確認できません')
  expect(props.onCreated).not.toHaveBeenCalled()
})

it('accepts an authorized replay after delegation and never displays transport secrets', async () => {
  vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(Response.json(people)).mockRejectedValueOnce(new Error('private-secret')).mockResolvedValueOnce(Response.json({ ...created, status: 'completed', targetUserId: 'carol', delegatedFromUserId: 'bob' }))
  render(<RequestComposer {...props} />); await fill()
  fireEvent.click(screen.getByRole('button', { name: '候補を生成して送信' }))
  expect(await screen.findByRole('alert')).not.toHaveTextContent('private-secret')
  fireEvent.click(screen.getByRole('button', { name: '候補を生成して送信' }))
  await waitFor(() => expect(props.onCreated).toHaveBeenCalledTimes(1))
})
