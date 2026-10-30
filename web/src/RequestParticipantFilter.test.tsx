import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { RequestBrowser } from './RequestBrowser'
import type { CoordinationRequest } from './bookingResponse'
afterEach(cleanup)
const base: CoordinationRequest = { id: 'r', requesterUserId: 'alice-other', targetUserId: 'bob', title: '会議', type: 'meeting', priority: 'normal', durationMinutes: 30, deadlineAt: '2030-02-01T00:00:00Z', createdAt: '2030-01-01T00:00:00Z', status: 'suggested', options: [] }
const values = [...Array.from({ length: 22 }, (_, i) => ({ ...base, id: `r${i}` })), { ...base, id: 'exact', requesterUserId: 'alice' }, { ...base, id: 'target', targetUserId: 'alice' }]
const rows = (items: CoordinationRequest[]) => <ul>{items.map(row => <li key={row.id}>{row.id}</li>)}</ul>
it('matches either participant exactly, composes filters, retains selection on refresh and resets scope', () => {
 const view = render(<RequestBrowser key="a" requests={values}>{rows}</RequestBrowser>)
 fireEvent.click(screen.getByRole('button', { name: '次のページ' }))
 fireEvent.change(screen.getByLabelText('参加者'), { target: { value: 'alice' } })
 expect(screen.getAllByRole('listitem').map(row => row.textContent)).toEqual(['exact', 'target'])
 fireEvent.change(screen.getByRole('searchbox'), { target: { value: 'exact' } })
 expect(screen.getByRole('listitem')).toHaveTextContent('exact')
 view.rerender(<RequestBrowser key="a" requests={[base]}>{rows}</RequestBrowser>)
 expect(screen.getByLabelText('参加者')).toHaveValue('alice')
 expect(screen.queryAllByRole('listitem')).toHaveLength(0)
 fireEvent.click(screen.getByRole('button', { name: '絞り込みを解除' }))
 expect(screen.getByRole('listitem')).toHaveTextContent('r')
 fireEvent.change(screen.getByLabelText('参加者'), { target: { value: 'bob' } })
 view.rerender(<RequestBrowser key="b" requests={values}>{rows}</RequestBrowser>)
 expect(screen.getByLabelText('参加者')).toHaveValue('')
})
it('fetches names only on explicit request and disambiguates the name with its ID', async () => {
 const load = vi.fn().mockResolvedValue({ alice: '田中' })
 render(<RequestBrowser requests={values} loadPeople={load}>{rows}</RequestBrowser>)
 expect(load).not.toHaveBeenCalled()
 fireEvent.click(screen.getByRole('button', { name: '検索用の氏名を取得' }))
 expect(await screen.findByRole('option', { name: '田中 (alice)' })).toHaveValue('alice')
})
