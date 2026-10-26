import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it } from 'vitest'
import { NotificationBrowser } from './NotificationBrowser'
import type { AppNotification } from './notificationResponse'
afterEach(cleanup)
const base: AppNotification = { id: 'a', requestId: 'r', type: 'request_accepted', message: '日時が確定', createdAt: '2030-01-01T00:00:00Z' }
const values = [base, { ...base, id: 'b', type: 'future_type' }, { ...base, id: 'c', readAt: '2030-01-01T00:00:01Z' }]
const rows = (items: AppNotification[]) => <ul>{items.map(row => <li key={row.id}>{row.id}</li>)}</ul>
it('combines type, unread and search; retains a selected type across refresh and clears by scope', () => {
 const view = render(<NotificationBrowser key="a" notifications={values}>{rows}</NotificationBrowser>)
 expect(screen.getByRole('option', { name: '日時の確定' })).toHaveValue('request_accepted')
 fireEvent.change(screen.getByRole('combobox'), { target: { value: 'request_accepted' } })
 fireEvent.click(screen.getByRole('checkbox'))
 fireEvent.change(screen.getByRole('searchbox'), { target: { value: '確定' } })
 expect(screen.getAllByRole('listitem')).toHaveLength(1)
 fireEvent.change(screen.getByRole('combobox'), { target: { value: 'future_type' } })
 expect(screen.getByRole('listitem')).toHaveTextContent('b')
 view.rerender(<NotificationBrowser key="a" notifications={[base]}>{rows}</NotificationBrowser>)
 expect(screen.getByRole('combobox')).toHaveValue('future_type')
 expect(screen.queryAllByRole('listitem')).toHaveLength(0)
 fireEvent.click(screen.getByRole('button', { name: '通知の絞り込みを解除' }))
 expect(screen.getByRole('combobox')).toHaveValue('')
 expect(screen.getByRole('checkbox')).not.toBeChecked()
 fireEvent.change(screen.getByRole('combobox'), { target: { value: 'request_accepted' } })
 view.rerender(<NotificationBrowser key="b" notifications={values}>{rows}</NotificationBrowser>)
 expect(screen.getByRole('combobox')).toHaveValue('')
 expect(screen.getAllByRole('listitem')).toHaveLength(3)
})
