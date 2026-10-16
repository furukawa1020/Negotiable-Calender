import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import App from './App'
import { readRequestList } from './requestList'

const request = { id: 'r', requesterUserId: 'demo-member', targetUserId: 'demo-manager', title: '期限切れ通信の古い依頼', type: 'meeting', status: 'cancelled', priority: 'normal', durationMinutes: 30, deadlineAt: '2030-01-01T00:00:00Z', createdAt: '2026-01-01T00:00:00Z', options: [] }
function deferred<T>() { let resolve!: (v: T) => void; const promise = new Promise<T>(done => { resolve = done }); return { promise, resolve } }
afterEach(() => { cleanup(); vi.useRealTimers(); vi.restoreAllMocks() })

it.each([
  ['依頼', 'fetch'], ['依頼', 'body'], ['送信済み', 'fetch'], ['送信済み', 'body'],
])('bounds %s list %s and ignores late data after retry', async (label, phase) => {
  const late = deferred<Response>(), body = deferred<object>(), response = Response.json({}); response.json = () => body.promise
  const fetch = vi.spyOn(globalThis, 'fetch').mockImplementationOnce(() => phase === 'fetch' ? late.promise : Promise.resolve(response)).mockImplementation(async () => Response.json({ requests: [] }))
  render(<App />)
  vi.useFakeTimers()
  await act(async () => { fireEvent.click(screen.getByRole('button', { name: label })) })
  await act(async () => { await vi.advanceTimersByTimeAsync(20_000) })
  expect(fetch.mock.calls[0][1]?.signal?.aborted).toBe(true)
  expect(screen.getByRole('alert')).toHaveTextContent('依頼を取得できませんでした。')
  expect(screen.queryByText(/取得しています/)).not.toBeInTheDocument()
  await act(async () => { fireEvent.click(screen.getByRole('button', { name: '更新' })) })
  await act(async () => { body.resolve({ requests: [request] }); late.resolve(Response.json({ requests: [request] })) })
  expect(screen.queryByText(request.title)).not.toBeInTheDocument()
  expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  expect(fetch).toHaveBeenCalledTimes(2)
  expect(vi.getTimerCount()).toBe(0)
})

it.each(['依頼', '送信済み'])('recovers %s from malformed list data', async label => {
  vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(Response.json({ requests: [{ ...request, options: null }] })).mockResolvedValueOnce(Response.json({ requests: [] }))
  render(<App />)
  fireEvent.click(screen.getByRole('button', { name: label }))
  expect(await screen.findByRole('alert')).toHaveTextContent('依頼を取得できませんでした。')
  fireEvent.click(screen.getByRole('button', { name: '更新' }))
  expect(await screen.findByText(label === '送信済み' ? '送信済み依頼はありません。' : '新しい依頼はありません。')).toBeInTheDocument()
})

it.each([null, {}, { requests: null }, { requests: [request, request] }, { requests: [{ ...request, title: {} }] }, { requests: [{ ...request, asyncMessage: {} }] }, { requests: [{ ...request, options: [null] }] }])('rejects unrenderable snapshots without leaking their contents', async value => {
  await expect(readRequestList(Response.json(value))).rejects.toThrow('invalid request list')
})

it('accepts empty and valid lists', async () => {
  await expect(readRequestList(Response.json({ requests: [] }))).resolves.toEqual([])
  await expect(readRequestList(Response.json({ requests: [request] }))).resolves.toEqual([request])
})
