import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { RequestComposer } from './RequestComposer'

const people = [{ id: 'alice', displayName: '自分' }, { id: 'bob', displayName: '依頼先B' }]
const props = { apiURL: 'http://localhost:8080', organizationID: 'org-1', requesterID: 'alice', initialTargetID: 'bob', demo: false, onClose: vi.fn(), onCreated: vi.fn() }
const json = (value: unknown, status = 200) => new Response(JSON.stringify(value), { status })

async function fill() {
  await waitFor(() => expect(screen.getByLabelText('依頼先')).toBeEnabled())
  fireEvent.change(screen.getByLabelText('依頼内容'), { target: { value: '設計確認' } })
  fireEvent.change(screen.getByLabelText('回答方法'), { target: { value: 'sync' } })
}

describe('RequestComposer', () => {
  afterEach(() => { vi.restoreAllMocks(); props.onClose.mockClear(); props.onCreated.mockClear() })

  it('sends a real selected recipient with an absolute deadline and API sync value', async () => {
    const fetch = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(json({ people })).mockResolvedValueOnce(json({ options: [{ id: 'one' }] }, 201))
    render(<RequestComposer {...props} />)
    await fill()
    expect(screen.queryByRole('option', { name: '自分' })).not.toBeInTheDocument()
    expect(screen.getByLabelText('依頼先')).toHaveValue('bob')
    fireEvent.click(screen.getByRole('button', { name: '候補を生成して送信' }))
    await waitFor(() => expect(props.onCreated).toHaveBeenCalledWith(1))
    const [, init] = fetch.mock.calls[1]
    expect(JSON.parse(String(init?.body))).toMatchObject({ targetUserId: 'bob', type: 'review', title: '設計確認', syncPreference: 'sync' })
    expect(Number.isFinite(Date.parse(JSON.parse(String(init?.body)).deadlineAt))).toBe(true)
    expect(init?.headers).toMatchObject({ 'X-Organization-ID': 'org-1', 'X-Demo-User-ID': 'alice', 'Idempotency-Key': expect.any(String) })
  })

  it('reuses the exact key and payload after a lost response', async () => {
    const fetch = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(json({ people })).mockRejectedValueOnce(new TypeError('network')).mockResolvedValueOnce(json({ options: [] }))
    render(<RequestComposer {...props} />)
    await fill()
    fireEvent.click(screen.getByRole('button', { name: '候補を生成して送信' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('同じ内容で再送')
    fireEvent.click(screen.getByRole('button', { name: '候補を生成して送信' }))
    await waitFor(() => expect(props.onCreated).toHaveBeenCalled())
    expect(fetch.mock.calls[2][1]?.body).toEqual(fetch.mock.calls[1][1]?.body)
    expect(fetch.mock.calls[2][1]?.headers).toEqual(fetch.mock.calls[1][1]?.headers)
  })

  it('uses a new key when the user changes a rejected command', async () => {
    const fetch = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(json({ people })).mockResolvedValueOnce(json({}, 422)).mockResolvedValueOnce(json({ options: [] }, 201))
    render(<RequestComposer {...props} />)
    await fill()
    fireEvent.click(screen.getByRole('button', { name: '候補を生成して送信' }))
    await screen.findByRole('alert')
    fireEvent.change(screen.getByLabelText('依頼内容'), { target: { value: '変更した依頼' } })
    fireEvent.click(screen.getByRole('button', { name: '候補を生成して送信' }))
    await waitFor(() => expect(props.onCreated).toHaveBeenCalled())
    expect(fetch.mock.calls[2][1]?.headers).not.toEqual(fetch.mock.calls[1][1]?.headers)
  })

  it('blocks double submit while the result is pending', async () => {
    let resolve!: (value: Response) => void
    const fetch = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(json({ people })).mockImplementationOnce(() => new Promise(done => { resolve = done }))
    render(<RequestComposer {...props} />)
    await fill()
    const form = screen.getByLabelText('依頼内容').closest('form')!
    fireEvent.submit(form); fireEvent.submit(form)
    expect(fetch).toHaveBeenCalledTimes(2)
    expect(screen.getByRole('button', { name: '送信中…' })).toBeDisabled()
    resolve(json({ options: [] }, 201))
    await waitFor(() => expect(props.onCreated).toHaveBeenCalledTimes(1))
  })

  it.each([{ people: [] }, { people: [{ id: 'alice', displayName: '自分' }] }])('does not default to self when the directory has no recipients', async ({ people }) => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(json({ people }))
    render(<RequestComposer {...props} />)
    expect(await screen.findByText(/依頼先となる管理職がいません/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '候補を生成して送信' })).toBeDisabled()
  })

  it('fails closed when recipient lookup fails and lets the user reload', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(json({}, 503)).mockResolvedValueOnce(json({ people }))
    render(<RequestComposer {...props} />)
    expect(await screen.findByRole('alert')).toHaveTextContent('依頼先を取得できません')
    expect(screen.getByRole('button', { name: '候補を生成して送信' })).toBeDisabled()
    fireEvent.click(screen.getByRole('button', { name: '依頼先を再読み込み' }))
    await waitFor(() => expect(screen.getByLabelText('依頼先')).toBeEnabled())
  })

  it('ignores the previous workspace response after remount', async () => {
    let resolve!: (value: Response) => void
    vi.spyOn(globalThis, 'fetch').mockImplementationOnce(() => new Promise(done => { resolve = done })).mockResolvedValueOnce(json({ people: [{ id: 'carol', displayName: '組織2の相手' }] }))
    const view = render(<RequestComposer key="org-1" {...props} />)
    view.rerender(<RequestComposer key="org-2" {...props} organizationID="org-2" initialTargetID="" />)
    await screen.findByRole('option', { name: '組織2の相手' })
    resolve(json({ people }))
    await waitFor(() => expect(screen.queryByRole('option', { name: '依頼先B' })).not.toBeInTheDocument())
    expect(screen.getByLabelText('依頼先')).toHaveValue('')
  })
})
