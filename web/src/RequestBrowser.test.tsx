import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it } from 'vitest'
import { RequestBrowser } from './RequestBrowser'
import { filterRequests } from './requestBrowserModel'
import type { CoordinationRequest } from './bookingResponse'

const base: CoordinationRequest = { id: 'r-1', requesterUserId: 'alice', targetUserId: 'bob', title: 'ＡＢＣ 設計レビュー', status: 'suggested', type: 'review', priority: 'normal', durationMinutes: 30, deadlineAt: '2030-02-01T00:00:00Z', createdAt: '2030-01-01T00:00:00Z', options: [] }
const requests = [base, { ...base, id: 'r-2', title: '確定した会議', status: 'accepted' }]
const rows = (values: CoordinationRequest[]) => <ul>{values.map(row => <li key={row.id}>{row.title}</li>)}</ul>
afterEach(cleanup)
it('combines normalized search and status without mutating requests', () => {
  expect(filterRequests(requests, ' abc ', 'suggested')).toEqual([base])
  expect(filterRequests(requests, 'ALICE', '')).toEqual(requests)
  expect(filterRequests(requests, 'r-2', '')).toEqual([requests[1]])
  expect(filterRequests(requests, 'bob', 'cancelled')).toEqual([])
  expect(requests).toHaveLength(2)
})
it('filters, announces counts and restores all rows', () => {
  render(<RequestBrowser requests={requests}>{rows}</RequestBrowser>)
  fireEvent.change(screen.getByRole('searchbox', { name: '依頼を検索' }), { target: { value: 'abc' } })
  expect(screen.getByText('2件中 1件を表示')).toHaveAttribute('aria-live', 'polite')
  fireEvent.change(screen.getByRole('combobox', { name: '状態' }), { target: { value: 'accepted' } })
  expect(screen.getByText(/条件に一致する依頼はありません/)).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '絞り込みを解除' }))
  expect(screen.getAllByRole('listitem')).toHaveLength(2)
})
it('resets private search state at an identity key boundary and filters refreshed data', () => {
  const view = render(<RequestBrowser key="org-a" requests={requests}>{rows}</RequestBrowser>)
  fireEvent.change(screen.getByRole('searchbox'), { target: { value: 'abc' } })
  view.rerender(<RequestBrowser key="org-a" requests={[requests[1]]}>{rows}</RequestBrowser>)
  expect(screen.queryAllByRole('listitem')).toHaveLength(0)
  view.rerender(<RequestBrowser key="org-b" requests={requests}>{rows}</RequestBrowser>)
  expect(screen.getByRole('searchbox')).toHaveValue('')
  expect(screen.getAllByRole('listitem')).toHaveLength(2)
})
