import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it } from 'vitest'
import { RequestBrowser } from './RequestBrowser'
import { filterRequests, orderRequests } from './requestBrowserModel'
import type { CoordinationRequest } from './bookingResponse'

const base: CoordinationRequest = { id: 'r-1', requesterUserId: 'alice', targetUserId: 'bob', title: 'ＡＢＣ 設計レビュー', status: 'suggested', type: 'review', priority: 'normal', durationMinutes: 30, deadlineAt: '2030-02-01T00:00:00Z', createdAt: '2030-01-01T00:00:00Z', options: [] }
const requests = [base, { ...base, id: 'r-2', title: '確定した会議', status: 'accepted' }]
const rows = (values: CoordinationRequest[]) => <ul>{values.map(row => <li key={row.id}>{row.title}</li>)}</ul>
afterEach(cleanup)
it('orders by deadline, priority or newest without changing the source and breaks ties by ID', () => {
  const a = { ...base, id: 'a', deadlineAt: '2030-02-03T00:00:00Z', priority: 'urgent' }
  const b = { ...base, id: 'b', createdAt: '2030-01-03T00:00:00Z', priority: 'low' }
  const c = { ...base, id: 'c', deadlineAt: '2030-02-02T00:00:00Z', createdAt: '2030-01-02T00:00:00Z' }
  const source = Object.freeze([c, a, b])
  for (const [mode, expected] of [['deadline', ['b', 'c', 'a']], ['priority', ['a', 'c', 'b']], ['newest', ['b', 'c', 'a']]] as const) {
    expect(orderRequests([...source], mode).map(row => row.id)).toEqual(expected)
  }
  expect(source.map(row => row.id)).toEqual(['c', 'a', 'b'])
  expect(orderRequests([{ ...base, id: 'z' }, { ...base, id: 'a' }], 'newest').map(row => row.id)).toEqual(['a', 'z'])
})
it('changes ordering without losing the active filter', () => {
  const later = { ...base, id: 'later', title: 'ＡＢＣ 追加レビュー', priority: 'urgent', deadlineAt: '2030-03-01T00:00:00Z' }
  render(<RequestBrowser requests={[base, later, requests[1]]}>{rows}</RequestBrowser>)
  fireEvent.change(screen.getByRole('searchbox'), { target: { value: 'abc' } })
  fireEvent.change(screen.getByRole('combobox', { name: '並び順' }), { target: { value: 'priority' } })
  expect(screen.getAllByRole('listitem').map(row => row.textContent)).toEqual([later.title, base.title])
  fireEvent.change(screen.getByRole('combobox', { name: '並び順' }), { target: { value: 'deadline' } })
  expect(screen.getAllByRole('listitem').map(row => row.textContent)).toEqual([base.title, later.title])
  expect(screen.getByText('3件中 2件を表示')).toHaveAttribute('aria-live', 'polite')
})
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
