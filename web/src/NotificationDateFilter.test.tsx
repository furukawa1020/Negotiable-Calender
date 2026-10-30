import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it } from 'vitest'
import { NotificationBrowser } from './NotificationBrowser'
import type { AppNotification } from './notificationResponse'
afterEach(cleanup)
const values: AppNotification[] = Array.from({ length: 23 }, (_, i) => ({ id: `n${i}`, requestId: 'r', type: 'request_received', message: '通知', createdAt: new Date(2030, 0, i === 22 ? 3 : 2, i === 21 ? 23 : 0, 59, 59).toISOString() }))
const rows = (items: AppNotification[]) => <ul>{items.map(row => <li key={row.id}>{row.id}</li>)}</ul>
it('filters inclusive local dates, resets pagination and scope, composes search and rejects inverted ranges', () => {
 const view = render(<NotificationBrowser key="a" notifications={values}>{rows}</NotificationBrowser>)
 fireEvent.click(screen.getByRole('button', { name: '次のページ' }))
 fireEvent.change(screen.getByLabelText('通知の開始日'), { target: { value: '2030-01-03' } })
 expect(screen.getByRole('listitem')).toHaveTextContent('n22')
 fireEvent.change(screen.getByLabelText('通知の終了日'), { target: { value: '2030-01-02' } })
 expect(screen.getByRole('alert')).toBeInTheDocument()
 expect(screen.queryAllByRole('listitem')).toHaveLength(0)
 fireEvent.change(screen.getByLabelText('通知の開始日'), { target: { value: '2030-01-02' } })
 expect(screen.getByText(/22件が一致/)).toBeInTheDocument()
 fireEvent.change(screen.getByRole('searchbox'), { target: { value: 'missing' } })
 expect(screen.queryAllByRole('listitem')).toHaveLength(0)
 fireEvent.click(screen.getByRole('button', { name: '通知の絞り込みを解除' }))
 expect(screen.getByLabelText('通知の終了日')).toHaveValue('')
 fireEvent.change(screen.getByLabelText('通知の開始日'), { target: { value: '2030-01-03' } })
 view.rerender(<NotificationBrowser key="b" notifications={values}>{rows}</NotificationBrowser>)
 expect(screen.getByLabelText('通知の開始日')).toHaveValue('')
 expect(screen.getAllByRole('listitem')).toHaveLength(20)
})
