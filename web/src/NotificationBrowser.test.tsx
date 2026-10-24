import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it } from 'vitest'
import { NotificationBrowser } from './NotificationBrowser'
import type { AppNotification } from './notificationResponse'

const base: AppNotification = { id: 'b', requestId: 'r-abc', type: 'request_changed', message: 'ＡＢＣ 日時変更', createdAt: '2030-01-01T00:00:00Z' }
const values = [base, { ...base, id: 'a', message: '回答あり', readAt: '2030-01-02T00:00:00Z' }, { ...base, id: 'c', message: '新しい通知', createdAt: '2030-01-03T00:00:00Z' }]
const rows = (items: AppNotification[]) => <ul>{items.map(item => <li key={item.id}>{item.message}</li>)}</ul>
afterEach(cleanup)
it('sorts a copy with deterministic ties and searches normalized AND terms', () => {
  render(<NotificationBrowser notifications={values}>{rows}</NotificationBrowser>)
  expect(screen.getAllByRole('listitem').map(row => row.textContent)).toEqual(['新しい通知', '回答あり', 'ＡＢＣ 日時変更'])
  expect(values.map(row => row.id)).toEqual(['b', 'a', 'c'])
  fireEvent.change(screen.getByRole('searchbox'), { target: { value: 'abc　日時' } })
  expect(screen.getAllByRole('listitem')).toHaveLength(1)
  fireEvent.change(screen.getByRole('searchbox'), { target: { value: 'missing' } })
  expect(screen.getByText('条件に一致する通知はありません')).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '通知の絞り込みを解除' }))
  expect(screen.getAllByRole('listitem')).toHaveLength(3)
})
it('updates unread results and resets filters at identity boundaries', () => {
  const view = render(<NotificationBrowser key="a" notifications={values}>{rows}</NotificationBrowser>)
  fireEvent.click(screen.getByRole('checkbox', { name: '未読のみ' }))
  expect(screen.getAllByRole('listitem')).toHaveLength(2)
  const read = values.map(item => ({ ...item, readAt: '2030-01-04T00:00:00Z' }))
  view.rerender(<NotificationBrowser key="a" notifications={read}>{rows}</NotificationBrowser>)
  expect(screen.queryAllByRole('listitem')).toHaveLength(0)
  view.rerender(<NotificationBrowser key="b" notifications={read}>{rows}</NotificationBrowser>)
  expect(screen.getByRole('checkbox')).not.toBeChecked()
  expect(screen.getAllByRole('listitem')).toHaveLength(3)
})
