import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it } from 'vitest'
import { RequestBrowser } from './RequestBrowser'
import { filterRequests } from './requestBrowserModel'
import type { CoordinationRequest } from './bookingResponse'

afterEach(cleanup)
const base: CoordinationRequest = { id: 'r', requesterUserId: 'a', targetUserId: 'b', title: '会議', type: 'meeting', priority: 'normal', durationMinutes: 30, deadlineAt: '2030-02-01T00:00:00Z', createdAt: '2030-01-01T00:00:00Z', status: 'suggested', options: [] }
it('separates unresolved states from all terminal states and composes filters', () => {
  const states = ['pending', 'suggested', 'accepted', 'delegated', 'async', 'declined', 'cancelled', 'expired', 'completed']
  const values = states.map(status => ({ ...base, id: status, status }))
  expect(filterRequests(values, '', 'active').map(row => row.status)).toEqual(states.slice(0, 4))
  expect(filterRequests(values, 'accepted', 'active', 'meeting', 'normal')).toEqual([values[2]])
  render(<RequestBrowser requests={values}>{rows => <ul>{rows.map(row => <li key={row.id}>{row.id}</li>)}</ul>}</RequestBrowser>)
  fireEvent.change(screen.getByRole('combobox', { name: '状態' }), { target: { value: 'active' } })
  expect(screen.getAllByRole('listitem')).toHaveLength(4)
  fireEvent.click(screen.getByRole('button', { name: '絞り込みを解除' }))
  expect(screen.getAllByRole('listitem')).toHaveLength(9)
})
