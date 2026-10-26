import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it } from 'vitest'
import { NotificationBrowser } from './NotificationBrowser'
import type { AppNotification } from './notificationResponse'
afterEach(cleanup)
const values: AppNotification[] = Array.from({ length: 41 }, (_, i) => ({ id: String(i).padStart(3, '0'), requestId: 'r', type: 'request_changed', message: `通知${i}`, createdAt: '2030-01-01T00:00:00Z' }))
const rows = (items: AppNotification[]) => <ul>{items.map(row => <li key={row.id}>{row.message}</li>)}</ul>
it('pages a loaded snapshot, resets on filters, clamps after refresh and discards obsolete pages', () => {
 const view = render(<NotificationBrowser notifications={values}>{rows}</NotificationBrowser>)
 expect(screen.getAllByRole('listitem')).toHaveLength(20)
 expect(screen.getByRole('button', { name: '前のページ' })).toBeDisabled()
 fireEvent.click(screen.getByRole('button', { name: '次のページ' }))
 expect(screen.getAllByRole('listitem')[0]).toHaveTextContent('通知20')
 fireEvent.click(screen.getByRole('button', { name: '次のページ' }))
 expect(screen.getAllByRole('listitem')).toHaveLength(1)
 expect(screen.getByRole('button', { name: '次のページ' })).toBeDisabled()
 view.rerender(<NotificationBrowser notifications={values.slice(0, 21)}>{rows}</NotificationBrowser>)
 expect(screen.getByText('2 / 2 ページ')).toBeInTheDocument()
 fireEvent.change(screen.getByRole('searchbox'), { target: { value: '通知0' } })
 expect(screen.getAllByRole('listitem')).toHaveLength(1)
 fireEvent.click(screen.getByRole('button', { name: '通知の絞り込みを解除' }))
 expect(screen.getByText('1 / 2 ページ')).toBeInTheDocument()
 fireEvent.click(screen.getByRole('button', { name: '次のページ' }))
 view.rerender(<NotificationBrowser notifications={[]}>{rows}</NotificationBrowser>)
 view.rerender(<NotificationBrowser notifications={values}>{rows}</NotificationBrowser>)
 expect(screen.getByText('1 / 3 ページ')).toBeInTheDocument()
})
