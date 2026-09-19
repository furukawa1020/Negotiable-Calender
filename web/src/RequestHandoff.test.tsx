import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { RequestHandoff } from './RequestHandoff'

const props = { apiURL: '', actor: 'bob', organizationID: 'org', requestID: 'r', requesterID: 'alice', disabled: false, onDone: vi.fn() }
const people = [{ id: 'alice', displayName: '依頼者' }, { id: 'bob', displayName: '自分' }, { id: 'carol', displayName: '引継ぎ担当' }]
const json = (body: object, status = 200) => new Response(JSON.stringify(body), { status })
afterEach(() => { vi.restoreAllMocks(); props.onDone.mockClear() })
async function select() {
  fireEvent.click(screen.getByRole('button', { name: '担当を引き継ぐ' }))
  await screen.findByRole('option', { name: '引継ぎ担当' })
  fireEvent.change(screen.getByLabelText('引継ぎ先'), { target: { value: 'carol' } })
}
it('loads on demand, excludes self and requester, and acknowledges the actual recipient', async () => {
  const fetch = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(json({ people })).mockResolvedValueOnce(json({ id: 'r', handedOff: true, delegatedUserId: 'carol' }))
  render(<RequestHandoff {...props} />)
  expect(fetch).not.toHaveBeenCalled()
  await select()
  expect(screen.queryByRole('option', { name: '自分' })).not.toBeInTheDocument()
  expect(screen.queryByRole('option', { name: '依頼者' })).not.toBeInTheDocument()
  expect(screen.getByText(/閲覧・回答できなくなります/)).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: 'この相手へ引き継ぐ' }))
  await waitFor(() => expect(props.onDone).toHaveBeenCalledWith('引継ぎ担当'))
  expect(fetch.mock.calls[1][1]).toMatchObject({ body: '{"delegateUserId":"carol"}', credentials: 'include', headers: { 'X-Organization-ID': 'org', 'X-Demo-User-ID': 'bob' } })
})
it('keeps the recipient fixed for a retry after an uncertain outcome', async () => {
  const fetch = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(json({ people })).mockRejectedValueOnce(new TypeError('network')).mockResolvedValueOnce(json({ id: 'r', handedOff: true, delegatedUserId: 'carol' }))
  render(<RequestHandoff {...props} />); await select()
  fireEvent.click(screen.getByRole('button', { name: 'この相手へ引き継ぐ' }))
  expect(await screen.findByRole('alert')).toHaveTextContent('同じ相手への再試行')
  expect(screen.getByLabelText('引継ぎ先')).toBeDisabled()
  fireEvent.click(screen.getByRole('button', { name: 'この相手へ引き継ぐ' }))
  await waitFor(() => expect(props.onDone).toHaveBeenCalledTimes(1))
  expect(fetch.mock.calls[1][1]?.body).toBe(fetch.mock.calls[2][1]?.body)
})
it('blocks synchronous duplicate submission and ignores a response after unmount', async () => {
  let done!: (response: Response) => void
  const fetch = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(json({ people })).mockImplementationOnce(() => new Promise(resolve => { done = resolve }))
  const view = render(<RequestHandoff {...props} />); await select()
  const form = screen.getByLabelText('引継ぎ先').closest('form')!
  fireEvent.submit(form); fireEvent.submit(form)
  expect(fetch).toHaveBeenCalledTimes(2)
  expect(screen.getByRole('button', { name: '引継ぎ中…' })).toBeDisabled()
  view.unmount(); expect(fetch.mock.calls[1][1]?.signal?.aborted).toBe(true)
  done(json({ id: 'r', handedOff: true, delegatedUserId: 'carol' }))
  await waitFor(() => expect(props.onDone).not.toHaveBeenCalled())
})
it('fails closed on directory failure and clears a removed recipient on reload', async () => {
  vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(json({ people })).mockResolvedValueOnce(json({}, 503))
  render(<RequestHandoff {...props} />); await select()
  fireEvent.click(screen.getByRole('button', { name: '引継ぎ先を再読み込み' }))
  await screen.findByRole('alert')
  expect(screen.getByLabelText('引継ぎ先')).toHaveValue('')
  expect(screen.getByRole('button', { name: 'この相手へ引き継ぐ' })).toBeDisabled()
})
