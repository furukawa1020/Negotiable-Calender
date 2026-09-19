import { act, renderHook } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { useRequestResolution } from './useRequestResolution'

const json = (body: object, status = 200) => new Response(JSON.stringify(body), { status })
describe('request resolution', () => {
  afterEach(() => { vi.restoreAllMocks(); vi.useRealTimers() })
  it('deduplicates synchronous repeated actions and includes the organization', async () => {
    let done!: (response: Response) => void
    const fetch = vi.spyOn(globalThis, 'fetch').mockImplementation(() => new Promise(resolve => { done = resolve }))
    const { result } = renderHook(() => useRequestResolution('/api', 'org', 'bob'))
    let first!: ReturnType<typeof result.current.resolve>
    await act(async () => {
      first = result.current.resolve('r', 'bob', 'async', ' answer ')
      expect(await result.current.resolve('r', 'bob', 'decline')).toBeUndefined()
    })
    expect(result.current.pending('r')).toBe(true)
    expect(fetch).toHaveBeenCalledTimes(1)
    expect(fetch.mock.calls[0][1]).toMatchObject({ credentials: 'include', body: '{"message":"answer"}', headers: { 'X-Organization-ID': 'org', 'X-Demo-User-ID': 'bob' } })
    await act(async () => { done(json({ id: 'r', status: 'async', asyncMessage: 'answer' })); await first })
    expect(result.current.pending('r')).toBe(false)
  })

  it('allows the exact response to be retried after a network failure', async () => {
    const fetch = vi.spyOn(globalThis, 'fetch').mockRejectedValueOnce(new TypeError('network')).mockResolvedValueOnce(json({ id: 'r', status: 'async', asyncMessage: 'answer' }))
    const { result } = renderHook(() => useRequestResolution('', 'org', 'bob'))
    await act(async () => { await expect(result.current.resolve('r', 'bob', 'async', 'answer')).rejects.toThrow('同じ操作・同じ回答文') })
    expect(result.current.pending('r')).toBe(false)
    await act(async () => { expect(await result.current.resolve('r', 'bob', 'async', 'answer')).toMatchObject({ status: 'async' }) })
    expect(fetch.mock.calls[0][1]?.body).toEqual(fetch.mock.calls[1][1]?.body)
  })

  it.each([
    [409, 'request_resolution_expired', '回答期限'],
    [409, 'request_resolution_conflict', '先に保存'],
    [403, '', '組織'], [503, '', '結果を確認できません'],
  ])('explains %s / %s without rendering server error text', async (status, code, expected) => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(json({ code, error: 'private server detail' }, Number(status)))
    const { result } = renderHook(() => useRequestResolution('', 'org', 'bob'))
    await act(async () => { await expect(result.current.resolve('r', 'bob', 'decline')).rejects.toThrow(String(expected)) })
  })

  it('ignores a completed response from a previous workspace', async () => {
    let done!: (response: Response) => void
    const fetch = vi.spyOn(globalThis, 'fetch').mockImplementationOnce(() => new Promise(resolve => { done = resolve })).mockResolvedValueOnce(json({ id: 'r', status: 'declined' }))
    const { result, rerender } = renderHook(({ org }) => useRequestResolution('', org, 'bob'), { initialProps: { org: 'one' } })
    let previous!: ReturnType<typeof result.current.resolve>
    act(() => { previous = result.current.resolve('r', 'bob', 'decline') })
    rerender({ org: 'two' })
    expect(result.current.pending('r')).toBe(false)
    expect(fetch.mock.calls[0][1]?.signal?.aborted).toBe(true)
    await act(async () => { done(json({ id: 'r', status: 'declined' })); expect(await previous).toBeUndefined() })
    await act(async () => { expect(await result.current.resolve('r', 'bob', 'decline')).toMatchObject({ status: 'declined' }) })
  })

  it('does not accept an unrelated success payload', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(json({ id: 'r', status: 'accepted' }))
    const { result } = renderHook(() => useRequestResolution('', 'org', 'bob'))
    await act(async () => { await expect(result.current.resolve('r', 'bob', 'decline')).rejects.toThrow('結果を確認できません') })
  })

  it('does not let an abandoned request unlock a newer command after switching back', async () => {
    let oldDone!: (response: Response) => void
    let newDone!: (response: Response) => void
    vi.spyOn(globalThis, 'fetch').mockImplementationOnce(() => new Promise(resolve => { oldDone = resolve })).mockImplementationOnce(() => new Promise(resolve => { newDone = resolve }))
    const { result, rerender } = renderHook(({ org }) => useRequestResolution('', org, 'bob'), { initialProps: { org: 'one' } })
    let oldRequest!: ReturnType<typeof result.current.resolve>
    act(() => { oldRequest = result.current.resolve('r', 'bob', 'decline') })
    rerender({ org: 'two' }); rerender({ org: 'one' })
    let newRequest!: ReturnType<typeof result.current.resolve>
    act(() => { newRequest = result.current.resolve('r', 'bob', 'decline') })
    await act(async () => { oldDone(json({ id: 'r', status: 'declined' })); expect(await oldRequest).toBeUndefined() })
    expect(result.current.pending('r')).toBe(true)
    await act(async () => { newDone(json({ id: 'r', status: 'declined' })); await newRequest })
    expect(result.current.pending('r')).toBe(false)
  })
})
