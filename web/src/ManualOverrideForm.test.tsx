import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { ManualOverrideForm } from './ManualOverrideForm'
import { overrideCommand } from './manualOverride'
const date = new Date(2030, 0, 2)
function deferred<T>() { let resolve!: (v: T) => void; const promise = new Promise<T>(done => { resolve = done }); return { promise, resolve } }
function setup() { const saved = vi.fn(); const current = vi.fn(() => true); const view = render(<ManualOverrideForm apiURL="" userID="owner" date={date} isCurrent={current} onSaved={saved} onClose={vi.fn()} />); return { saved, current, view } }
afterEach(() => { cleanup(); vi.useRealTimers(); vi.restoreAllMocks() })
it.each([['bad', '16:00'], ['25:00', '26:00'], ['16:00', '15:30'], ['16:00', '16:00']])('rejects invalid window %s-%s', (a,b) => { expect(() => overrideCommand(date, a,b,'available')).toThrow() })
it('accepts only matching stored override evidence', async () => {
  vi.spyOn(globalThis, 'fetch').mockImplementation(async (_url, init) => Response.json({ id: 'saved', userId: 'owner', ...JSON.parse(String(init?.body)) }))
  const h = setup(); fireEvent.click(screen.getByRole('button', { name: '上書きを保存' })); await act(async () => {})
  expect(h.saved).toHaveBeenCalledWith('15:30', '16:00', 'available')
})
it.each([{}, { id: 'saved', userId: 'foreign', ...overrideCommand(date,'15:30','16:00','available') }, { id: 'saved', userId: 'owner', ...overrideCommand(date,'14:00','16:00','available') }])('does not claim success from unrelated or incomplete evidence', async payload => {
  vi.spyOn(globalThis, 'fetch').mockResolvedValue(Response.json(payload)); const h = setup()
  fireEvent.click(screen.getByRole('button', { name: '上書きを保存' })); await screen.findByRole('alert')
  expect(h.saved).not.toHaveBeenCalled(); expect(screen.getByRole('button', { name: '上書きを保存' })).toBeDisabled()
})
it.each(['fetch','body'])('bounds %s without automatic or accidental repeated submission', async phase => {
  const late = deferred<Response>(), body = deferred<object>(), response = Response.json({}); response.json = () => body.promise
  const fetch = vi.spyOn(globalThis, 'fetch').mockImplementation(() => phase === 'fetch' ? late.promise : Promise.resolve(response)); const h = setup(); vi.useFakeTimers()
  await act(async () => { fireEvent.click(screen.getByRole('button', { name: '上書きを保存' })) })
  await act(async () => { await vi.advanceTimersByTimeAsync(20_000) })
  expect(screen.getByRole('alert')).toHaveTextContent('保存された可能性')
  fireEvent.click(screen.getByRole('button', { name: '上書きを保存' })); expect(fetch).toHaveBeenCalledTimes(1)
  const payload = { id: 'saved', userId: 'owner', ...overrideCommand(date,'15:30','16:00','available') }
  await act(async () => { late.resolve(Response.json(payload)); body.resolve(payload) })
  expect(h.saved).not.toHaveBeenCalled()
})
it.each(['unmount','scope'])('ignores late acknowledgements after %s', async boundary => {
  const body = deferred<object>(), response = Response.json({}); response.json = () => body.promise
  vi.spyOn(globalThis, 'fetch').mockResolvedValue(response); const h = setup()
  fireEvent.click(screen.getByRole('button', { name: '上書きを保存' })); await act(async () => {})
  if (boundary === 'unmount') h.view.unmount(); else h.current.mockReturnValue(false)
  await act(async () => { body.resolve({ id: 'saved', userId: 'owner', ...overrideCommand(date,'15:30','16:00','available') }) })
  expect(h.saved).not.toHaveBeenCalled()
})
