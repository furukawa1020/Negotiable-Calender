import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it } from 'vitest'
import { NotificationBrowser } from './NotificationBrowser'
import type { AppNotification } from './notificationResponse'
afterEach(cleanup)
const values: AppNotification[] = Array.from({ length: 23 }, (_, i) => ({ id: `n${String(i).padStart(2, '0')}`, requestId: 'r', type: 'request_received', message: '通知', createdAt: new Date(2030, 0, i + 1).toISOString() }))
const rows = (items: AppNotification[]) => <ul>{items.map(row => <li key={row.id}>{row.id}</li>)}</ul>
it('orders oldest first without mutation, resets page, preserves filters and resets on scope change', () => {
 const original = [...values]
 const view = render(<NotificationBrowser key="a" notifications={values}>{rows}</NotificationBrowser>)
 expect(screen.getAllByRole('listitem')[0]).toHaveTextContent('n22')
 fireEvent.click(screen.getByRole('button', { name: '次のページ' }))
 fireEvent.change(screen.getByLabelText('通知の並び順'), { target: { value: 'oldest' } })
 expect(screen.getAllByRole('listitem')[0]).toHaveTextContent('n00')
 expect(screen.getByText('1 / 2 ページ')).toBeInTheDocument()
 fireEvent.change(screen.getByRole('searchbox'), { target: { value: '通知' } })
 expect(screen.getByLabelText('通知の並び順')).toHaveValue('oldest')
 expect(values).toEqual(original)
 view.rerender(<NotificationBrowser key="b" notifications={values}>{rows}</NotificationBrowser>)
 expect(screen.getByLabelText('通知の並び順')).toHaveValue('newest')
})
it('uses stable IDs for equal timestamps in either direction', () => {
 render(<NotificationBrowser notifications={[{ ...values[0], id: 'b' }, { ...values[0], id: 'a' }]}>{rows}</NotificationBrowser>)
 expect(screen.getAllByRole('listitem').map(row => row.textContent)).toEqual(['a', 'b'])
 fireEvent.change(screen.getByLabelText('通知の並び順'), { target: { value: 'oldest' } })
 expect(screen.getAllByRole('listitem').map(row => row.textContent)).toEqual(['a', 'b'])
})
