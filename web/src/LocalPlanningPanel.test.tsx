import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { LocalPlanning } from './LocalPlanningPanel'
import type { LocalSession } from './localPlanning'

const fixture = () => ({ revision: 'a'.repeat(64), expiresAt: new Date(Date.now() + 60000).toISOString(), candidates: [{ id: 'c1', startAt: new Date(Date.now() + 3600000).toISOString(), endAt: new Date(Date.now() + 5400000).toISOString() }] })
const mockModel = () => {
  const session = { prompt: vi.fn().mockResolvedValue('{"candidateIds":["c1"]}'), destroy: vi.fn() }
  const model = { availability: vi.fn().mockResolvedValue('available'), create: vi.fn().mockResolvedValue(session) }
  vi.stubGlobal('LanguageModel', model)
  return { session, model }
}
const open = async () => { fireEvent.click(screen.getByRole('button', { name: '端末内 AI で候補を比べる' })); await screen.findByText('処理する候補（端末のタイムゾーンで表示）：') }
const compare = () => { fireEvent.click(screen.getByRole('checkbox')); fireEvent.click(screen.getByRole('button', { name: '同意して端末内で比較' })) }
afterEach(() => { cleanup(); vi.useRealTimers(); vi.unstubAllGlobals() })

describe('LocalPlanning', () => {
  it('requires explicit consent; checks before and after inference; does not fetch a cloud model', async () => {
    const { session, model } = mockModel()
    const fetch = vi.fn(); vi.stubGlobal('fetch', fetch)
    const value = fixture(); const load = vi.fn().mockResolvedValue(value)
    render(<LocalPlanning loadPreview={load} />)
    expect(load).not.toHaveBeenCalled()
    await open()
    expect(model.create).not.toHaveBeenCalled()
    expect(screen.getByRole('button', { name: '同意して端末内で比較' })).toBeDisabled()
    compare()
    await screen.findByText('AI の比較結果（予約ではありません）')
    expect(load).toHaveBeenCalledTimes(3)
    expect(session.prompt).toHaveBeenCalledTimes(1)
    expect(session.destroy).toHaveBeenCalled()
    expect(fetch).not.toHaveBeenCalled()
    expect(screen.getByRole('checkbox')).not.toBeChecked()
  })
  it('uses the ordinary flow on unsupported devices', async () => {
    vi.stubGlobal('LanguageModel', undefined)
    render(<LocalPlanning loadPreview={vi.fn().mockResolvedValue(fixture())} />)
    await open()
    expect(screen.getByText(/この環境では端末内 AI/)).toBeInTheDocument()
    expect(screen.queryByRole('checkbox')).not.toBeInTheDocument()
  })
  it.each([2, 3])('discards source changes at check %i', async (check) => {
    const { session } = mockModel(); const value = fixture()
    let calls = 0
    render(<LocalPlanning loadPreview={vi.fn(async () => ++calls === check ? { ...value, revision: 'b'.repeat(64) } : value)} />)
    await open(); compare()
    expect(await screen.findByRole('alert')).toHaveTextContent('空き状況が変わった')
    expect(screen.queryByText('AI の比較結果（予約ではありません）')).not.toBeInTheDocument()
    expect(session.prompt).toHaveBeenCalledTimes(check === 2 ? 0 : 1)
    expect(session.destroy).toHaveBeenCalled()
  })
  it('rejects invented output without displaying raw model text', async () => {
    const { session } = mockModel(); session.prompt.mockResolvedValue('{"candidateIds":["secret-invented-id"]}')
    render(<LocalPlanning loadPreview={vi.fn().mockResolvedValue(fixture())} />)
    await open(); compare()
    await screen.findByRole('alert')
    expect(screen.queryByText(/secret-invented/)).not.toBeInTheDocument()
    expect(session.destroy).toHaveBeenCalled()
  })
  it('cancels a pending session on close and never revives late output', async () => {
    const { session } = mockModel(); let resolve!: (value: string) => void
    session.prompt.mockImplementation(() => new Promise<string>((done) => { resolve = done }))
    render(<LocalPlanning loadPreview={vi.fn().mockResolvedValue(fixture())} />)
    await open(); compare(); await waitFor(() => expect(session.prompt).toHaveBeenCalled())
    const signal = session.prompt.mock.calls[0][1].signal as AbortSignal
    fireEvent.click(screen.getByRole('button', { name: '中断して閉じる' }))
    expect(signal.aborted).toBe(true)
    expect(session.destroy).toHaveBeenCalled()
    await act(async () => resolve('{"candidateIds":["c1"]}'))
    expect(screen.queryByText('AI の比較結果（予約ではありません）')).not.toBeInTheDocument()
  })
  it('destroys a session that arrives after unmount or identity re-key', async () => {
    const { model, session } = mockModel(); let resolve!: (value: LocalSession) => void
    model.create.mockImplementation(() => new Promise<LocalSession>((done) => { resolve = done }))
    const { unmount } = render(<LocalPlanning loadPreview={vi.fn().mockResolvedValue(fixture())} />)
    await open(); compare(); unmount()
    expect(model.create.mock.calls[0][0].signal.aborted).toBe(true)
    await act(async () => resolve(session))
    expect(session.destroy).toHaveBeenCalled()
    expect(session.prompt).not.toHaveBeenCalled()
  })
  it('times out an unresponsive preview fetch', async () => {
    vi.useFakeTimers()
    render(<LocalPlanning loadPreview={() => new Promise(() => {})} />)
    fireEvent.click(screen.getByRole('button', { name: '端末内 AI で候補を比べる' }))
    await act(async () => { await vi.advanceTimersByTimeAsync(15001) })
    expect(screen.getByRole('alert')).toHaveTextContent('中断')
    expect(screen.getByRole('button', { name: '閉じる' })).toBeEnabled()
  })
  it('removes previously displayed ranking when the snapshot expires', async () => {
    mockModel(); const value = fixture()
    render(<LocalPlanning loadPreview={vi.fn().mockResolvedValue(value)} />)
    await open(); compare(); await screen.findByText('AI の比較結果（予約ではありません）')
    // Trigger a real short expiry with a fresh keyed component under fake timers.
    cleanup(); vi.useFakeTimers()
    const short = { ...value, expiresAt: new Date(Date.now() + 50).toISOString() }
    render(<LocalPlanning loadPreview={vi.fn().mockResolvedValue(short)} />)
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: '端末内 AI で候補を比べる' })) })
    await act(async () => compare())
    expect(screen.getByText('AI の比較結果（予約ではありません）')).toBeInTheDocument()
    await act(async () => { await vi.advanceTimersByTimeAsync(51) })
    expect(screen.queryByText('AI の比較結果（予約ではありません）')).not.toBeInTheDocument()
    expect(screen.queryByRole('checkbox')).not.toBeInTheDocument()
  })
})
