import { afterEach, describe, expect, it, vi } from 'vitest'
import { fetchBookingResult, readCancellation, readCompletion } from './bookingTransport'

describe('booking transport deadline', () => {
  it('binds completion acknowledgement to the request and selected meeting', async () => {
    for (const value of [null, {}, { id: 'r', status: 'completed', acceptedOptionId: 'other' }, { id: 'other', status: 'completed', acceptedOptionId: 'o' }, { id: 'r', status: 'accepted', acceptedOptionId: 'o' }]) {
      await expect(readCompletion(Response.json(value), 'r', 'o')).rejects.toThrow()
    }
    await expect(readCompletion(Response.json({ id: 'r', status: 'completed', acceptedOptionId: 'o' }), 'r', 'o')).resolves.toBe(true)
  })
  afterEach(() => { vi.useRealTimers(); vi.restoreAllMocks() })

  it('keeps credentials and identity headers, decodes once, and clears the deadline', async () => {
    vi.useFakeTimers()
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(Response.json({ id: 'booking', status: 'cancelled' }))
    expect(await fetchBookingResult('/booking', { method: 'POST', headers: { 'X-Organization-ID': 'org' }, body: '{}' }, response => readCancellation(response, 'booking'), () => true)).toBe(true)
    expect(fetchMock).toHaveBeenCalledWith('/booking', expect.objectContaining({ method: 'POST', credentials: 'include', headers: { 'X-Organization-ID': 'org' }, body: '{}', signal: expect.any(AbortSignal) }))
    expect(vi.getTimerCount()).toBe(0)
  })

  it.each([401, 403, 409, 500])('does not decode or retry HTTP %s', async status => {
    vi.useFakeTimers()
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(null, { status }))
    const read = vi.fn()
    await expect(fetchBookingResult('/booking', {}, read, () => true)).rejects.toThrow('unconfirmed')
    expect(read).not.toHaveBeenCalled()
    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(vi.getTimerCount()).toBe(0)
  })

  it('cleans the deadline on a network or JSON error', async () => {
    vi.useFakeTimers()
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockRejectedValueOnce(new TypeError('offline')).mockResolvedValueOnce(new Response('not JSON'))
    await expect(fetchBookingResult('/booking', {}, response => response.json(), () => true)).rejects.toThrow('offline')
    expect(vi.getTimerCount()).toBe(0)
    await expect(fetchBookingResult('/booking', {}, response => response.json(), () => true)).rejects.toThrow()
    expect(fetchMock).toHaveBeenCalledTimes(2)
    expect(vi.getTimerCount()).toBe(0)
  })

  it('does not start a request for an obsolete lifetime', async () => {
    vi.useFakeTimers()
    const fetchMock = vi.spyOn(globalThis, 'fetch')
    expect(await fetchBookingResult('/booking', {}, response => response.json(), () => false)).toBeUndefined()
    expect(fetchMock).not.toHaveBeenCalled()
    expect(vi.getTimerCount()).toBe(0)
  })
})
