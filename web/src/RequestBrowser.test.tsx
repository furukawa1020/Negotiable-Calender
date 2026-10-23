import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it } from 'vitest'
import { RequestBrowser } from './RequestBrowser'
import { filterRequests, orderRequests } from './requestBrowserModel'
import type { CoordinationRequest } from './bookingResponse'

const base: CoordinationRequest = { id: 'r-1', requesterUserId: 'alice', targetUserId: 'bob', title: 'ＡＢＣ 設計レビュー', status: 'suggested', type: 'review', priority: 'normal', durationMinutes: 30, deadlineAt: '2030-02-01T00:00:00Z', createdAt: '2030-01-01T00:00:00Z', options: [] }
const requests = [base, { ...base, id: 'r-2', title: '確定した会議', status: 'accepted' }]
const rows = (values: CoordinationRequest[]) => <ul>{values.map(row => <li key={row.id}>{row.title}</li>)}</ul>
afterEach(cleanup)
it('searches normalized names and AND keywords across fields without mutating data', () => {
  expect(filterRequests(requests, 'ＡＢＣ　田中', '', '', '', { bob: '田中 花子' })).toEqual([base])
  expect(filterRequests(requests, 'abc alice', '')).toEqual([base])
  expect(filterRequests(requests, 'abc missing', '', '', '', { bob: '田中' })).toEqual([])
  expect(filterRequests([{ ...base, requesterUserId: 'toString' }], 'native code', '')).toEqual([])
})
it('loads names explicitly, retries errors and discards names across identity keys', async () => {
  let fail = true
  const load = async () => { if (fail) throw new Error('private'); return { bob: '田中 花子' } }
  const view = render(<RequestBrowser key="a" requests={requests} loadPeople={load}>{rows}</RequestBrowser>)
  fireEvent.change(screen.getByRole('searchbox'), { target: { value: '田中 abc' } })
  expect(screen.queryAllByRole('listitem')).toHaveLength(0)
  fireEvent.click(screen.getByRole('button', { name: '検索用の氏名を取得' }))
  await screen.findByText(/氏名を取得できませんでした/)
  fail = false
  fireEvent.click(screen.getByRole('button', { name: '検索用の氏名を取得' }))
  await screen.findByText('組織の氏名を検索に使えます')
  expect(screen.getAllByRole('listitem')).toHaveLength(1)
  view.rerender(<RequestBrowser key="b" requests={requests}>{rows}</RequestBrowser>)
  fireEvent.change(screen.getByRole('searchbox'), { target: { value: '田中' } })
  expect(screen.queryAllByRole('listitem')).toHaveLength(0)
})
it('ignores a late directory response after identity replacement', async () => {
  let resolve!: (names: Record<string, string>) => void
  const load = () => new Promise<Record<string, string>>(done => { resolve = done })
  const view = render(<RequestBrowser key="a" requests={requests} loadPeople={load}>{rows}</RequestBrowser>)
  fireEvent.click(screen.getByRole('button', { name: '検索用の氏名を取得' }))
  view.rerender(<RequestBrowser key="b" requests={requests} loadPeople={load}>{rows}</RequestBrowser>)
  await act(async () => { resolve({ bob: '以前の利用者' }) })
  expect(screen.queryByText('組織の氏名を検索に使えます')).not.toBeInTheDocument()
  fireEvent.change(screen.getByRole('searchbox'), { target: { value: '以前の利用者' } })
  expect(screen.queryAllByRole('listitem')).toHaveLength(0)
})
it('combines type and priority with search and status and clears all filters', () => {
  const urgent = { ...base, id: 'urgent', priority: 'urgent' }
  const meeting = { ...urgent, id: 'meeting', type: 'meeting' }
  const values = [base, urgent, meeting, requests[1]]
  expect(filterRequests(values, 'abc', 'suggested', 'review', 'urgent')).toEqual([urgent])
  render(<RequestBrowser requests={values}>{rows}</RequestBrowser>)
  fireEvent.change(screen.getByRole('combobox', { name: '依頼種別' }), { target: { value: 'review' } })
  fireEvent.change(screen.getByRole('combobox', { name: '優先度' }), { target: { value: 'urgent' } })
  expect(screen.getAllByRole('listitem')).toHaveLength(1)
  fireEvent.change(screen.getByRole('combobox', { name: '状態' }), { target: { value: 'accepted' } })
  expect(screen.queryAllByRole('listitem')).toHaveLength(0)
  fireEvent.click(screen.getByRole('button', { name: '絞り込みを解除' }))
  expect(screen.getByRole('combobox', { name: '依頼種別' })).toHaveValue('')
  expect(screen.getByRole('combobox', { name: '優先度' })).toHaveValue('')
  expect(screen.getAllByRole('listitem')).toHaveLength(4)
})
it('paginates loaded rows, resets after filtering and clamps on refreshed data', () => {
  const many = Array.from({ length: 41 }, (_, i) => ({ ...base, id: `r-${String(i).padStart(3, '0')}`, title: `依頼${i}` }))
  const view = render(<RequestBrowser requests={many}>{rows}</RequestBrowser>)
  expect(screen.getAllByRole('listitem')).toHaveLength(20)
  expect(screen.getByRole('button', { name: '前のページ' })).toBeDisabled()
  fireEvent.click(screen.getByRole('button', { name: '次のページ' }))
  expect(screen.getByText('2 / 3 ページ')).toBeInTheDocument()
  expect(screen.getAllByRole('listitem')[0]).toHaveTextContent('依頼20')
  fireEvent.click(screen.getByRole('button', { name: '次のページ' }))
  expect(screen.getAllByRole('listitem')).toHaveLength(1)
  expect(screen.getByRole('button', { name: '次のページ' })).toBeDisabled()
  view.rerender(<RequestBrowser requests={many.slice(0, 21)}>{rows}</RequestBrowser>)
  expect(screen.getByText('2 / 2 ページ')).toBeInTheDocument()
  fireEvent.change(screen.getByRole('searchbox'), { target: { value: '依頼0' } })
  expect(screen.getAllByRole('listitem')).toHaveLength(1)
  fireEvent.click(screen.getByRole('button', { name: '絞り込みを解除' }))
  expect(screen.getByText('1 / 2 ページ')).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '次のページ' }))
  fireEvent.change(screen.getByRole('combobox', { name: '並び順' }), { target: { value: 'deadline' } })
  expect(screen.getByText('1 / 2 ページ')).toBeInTheDocument()
  view.rerender(<RequestBrowser requests={[]}>{rows}</RequestBrowser>)
  expect(screen.queryByRole('navigation', { name: '依頼のページ送り' })).not.toBeInTheDocument()
  view.rerender(<RequestBrowser requests={many}>{rows}</RequestBrowser>)
  expect(screen.getByText('1 / 3 ページ')).toBeInTheDocument()
})
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
