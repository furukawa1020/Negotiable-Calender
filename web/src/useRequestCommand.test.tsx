import { act, renderHook } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { useRequestResolution } from './useRequestResolution'

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>(done => { resolve = done })
  return { promise, resolve }
}
type Action = 'accept' | 'async' | 'decline' | 'cancel'
type Hook = ReturnType<typeof useRequestResolution>
const invoke = (hook: Hook, action: Action, id = 'r', active = () => true) => action === 'accept'
  ? hook.accept(id, 'bob', 'chosen', active) : hook.resolve(id, 'bob', action, action === 'async' ? 'reply' : undefined)
const payload = (action: Action, id = 'r') => ({ id, status: { accept: 'accepted', async: 'async', decline: 'declined', cancel: 'cancelled' }[action], acceptedOptionId: 'chosen', asyncMessage: 'reply' })

describe('shared request command pipeline', () => {
  afterEach(() => { vi.useRealTimers(); vi.restoreAllMocks() })

  describe.each(['response', 'success body', 'error body'])('deadline during %s', phase => {
    it.each(['accept', 'async', 'decline', 'cancel'] as const)('recovers %s and does not let a late result unlock its retry', async action => {
      vi.useFakeTimers()
      const first = deferred<Response>(), body = deferred<object>(), retry = deferred<Response>()
      const response = Response.json(phase === 'error body' ? { code: 'booking_conflict' } : payload(action), { status: phase === 'error body' ? 409 : 200 })
      response.json = () => body.promise
      const fetch = vi.spyOn(globalThis, 'fetch').mockImplementationOnce(() => phase === 'response' ? first.promise : Promise.resolve(response)).mockImplementationOnce(() => retry.promise)
      const { result } = renderHook(() => useRequestResolution('', 'org', 'bob'))
      let command!: Promise<unknown>
      await act(async () => { command = invoke(result.current, action).catch(error => error) })
      expect(result.current.pending('r')).toBe(true)
      await act(async () => { await vi.advanceTimersByTimeAsync(20_000) })
      expect(await command).toBeInstanceOf(Error)
      expect(result.current.pending('r')).toBe(false)
      expect(fetch.mock.calls[0][1]?.signal?.aborted).toBe(true)
      expect(fetch).toHaveBeenCalledTimes(1)
      let retried!: Promise<unknown>
      act(() => { retried = invoke(result.current, action) })
      await act(async () => {
        if (phase === 'response') first.resolve(Response.json(payload(action)))
        else body.resolve(phase === 'error body' ? { code: 'booking_conflict' } : payload(action))
      })
      expect(result.current.pending('r')).toBe(true)
      await act(async () => { retry.resolve(Response.json(payload(action))); expect(await retried).toMatchObject(payload(action).status === 'accepted' ? { acceptedOptionId: 'chosen' } : { status: payload(action).status }) })
      expect(result.current.pending('r')).toBe(false)
      expect(vi.getTimerCount()).toBe(0)
    })
  })

  it.each(['accept', 'decline'] as const)('blocks synchronous duplicate and competing actions while %s is pending', async action => {
    const pending = deferred<Response>()
    const fetch = vi.spyOn(globalThis, 'fetch').mockImplementation(() => pending.promise)
    const { result } = renderHook(() => useRequestResolution('/api', 'org', 'bob'))
    let first!: Promise<unknown>
    await act(async () => {
      first = invoke(result.current, action, 'r /?')
      expect(await invoke(result.current, 'accept', 'r /?')).toBeUndefined()
      expect(await invoke(result.current, 'decline', 'r /?')).toBeUndefined()
      expect(await invoke(result.current, 'async', 'r /?')).toBeUndefined()
    })
    expect(fetch).toHaveBeenCalledTimes(1)
    expect(fetch.mock.calls[0][0]).toBe(`/api/api/v1/requests/r%20%2F%3F/${action}`)
    expect(fetch.mock.calls[0][1]).toMatchObject({ credentials: 'include', headers: { 'X-Demo-User-ID': 'bob', 'X-Organization-ID': 'org' } })
    await act(async () => { pending.resolve(Response.json(payload(action, 'r /?'))); await first })
    expect(result.current.pending('r /?')).toBe(false)
  })

  it('keeps different request locks independent when the first finishes', async () => {
    const first = deferred<Response>(), second = deferred<Response>()
    vi.spyOn(globalThis, 'fetch').mockImplementationOnce(() => first.promise).mockImplementationOnce(() => second.promise)
    const { result } = renderHook(() => useRequestResolution('', 'org', 'bob'))
    let a!: Promise<unknown>, b!: Promise<unknown>
    act(() => { a = invoke(result.current, 'accept', 'a'); b = invoke(result.current, 'accept', 'b') })
    expect(result.current.pending('a')).toBe(true)
    expect(result.current.pending('b')).toBe(true)
    await act(async () => { first.resolve(Response.json(payload('accept', 'a'))); await a })
    expect(result.current.pending('a')).toBe(false)
    expect(result.current.pending('b')).toBe(true)
    await act(async () => { second.resolve(Response.json(payload('accept', 'b'))); await b })
    expect(result.current.pending('b')).toBe(false)
  })

  it('aborts and settles on unmount even if fetch ignores abort', async () => {
    vi.useFakeTimers()
    const fetch = vi.spyOn(globalThis, 'fetch').mockImplementation(() => new Promise(() => {}))
    const { result, unmount } = renderHook(() => useRequestResolution('', 'org', 'bob'))
    let pending!: Promise<unknown>
    act(() => { pending = invoke(result.current, 'accept') })
    await act(async () => { unmount(); expect(await pending).toBeUndefined() })
    expect(fetch.mock.calls[0][1]?.signal?.aborted).toBe(true)
    expect(vi.getTimerCount()).toBe(0)
  })

  it('cannot let an abandoned acceptance unlock a new one after switching back', async () => {
    const old = deferred<Response>(), next = deferred<Response>()
    vi.spyOn(globalThis, 'fetch').mockImplementationOnce(() => old.promise).mockImplementationOnce(() => next.promise)
    const { result, rerender } = renderHook(({ org }) => useRequestResolution('', org, 'bob'), { initialProps: { org: 'one' } })
    let previous!: Promise<unknown>, current!: Promise<unknown>
    act(() => { previous = invoke(result.current, 'accept') })
    rerender({ org: 'two' }); rerender({ org: 'one' })
    act(() => { current = invoke(result.current, 'accept') })
    await act(async () => { old.resolve(Response.json(payload('accept'))); expect(await previous).toBeUndefined() })
    expect(result.current.pending('r')).toBe(true)
    await act(async () => { next.resolve(Response.json(payload('accept'))); await current })
    expect(result.current.pending('r')).toBe(false)
  })

  it('honors synchronous account invalidation before effect cleanup or JSON decoding', async () => {
    const pending = deferred<Response>()
    const response = Response.json(payload('accept'))
    const read = vi.spyOn(response, 'json')
    vi.spyOn(globalThis, 'fetch').mockImplementationOnce(() => pending.promise)
    const { result } = renderHook(() => useRequestResolution('', 'org', 'bob'))
    let active = true
    let command!: Promise<unknown>
    act(() => { command = invoke(result.current, 'accept', 'r', () => active) })
    active = false
    await act(async () => { pending.resolve(response); expect(await command).toBeUndefined() })
    expect(read).not.toHaveBeenCalled()
  })

  it.each([
    ['candidate_expired', '開始済み'], ['candidate_invalid', '確定できません'],
    ['availability_changed', '同期を確認できません'], ['booking_conflict', '同時更新'],
    ['private error text', '依頼の状態が変わりました'], ['__proto__', '依頼の状態が変わりました'], ['toString', '依頼の状態が変わりました'],
  ])('preserves static acceptance conflict guidance for %s', async (code, expected) => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(Response.json({ code, error: 'secret provider data' }, { status: 409 }))
    const { result } = renderHook(() => useRequestResolution('', 'org', 'bob'))
    await act(async () => { await expect(invoke(result.current, 'accept')).rejects.toThrow(expected) })
    expect(result.current.pending('r')).toBe(false)
  })

  it('does not leak raw transport error messages', async () => {
    vi.spyOn(globalThis, 'fetch').mockRejectedValue(new Error('secret provider data'))
    const { result } = renderHook(() => useRequestResolution('', 'org', 'bob'))
    await act(async () => { await expect(invoke(result.current, 'accept')).rejects.toThrow('最新状態を確認') })
    await act(async () => { await expect(invoke(result.current, 'async')).rejects.toThrow('結果を確認できません') })
  })
})
