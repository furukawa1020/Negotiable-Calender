import { expect, it } from 'vitest'
import { orderRequests } from './requestBrowserModel'
import type { CoordinationRequest } from './bookingResponse'

const base: CoordinationRequest = { id: 'r', requesterUserId: 'a', targetUserId: 'b', title: '会議', type: 'meeting', priority: 'normal', durationMinutes: 30, deadlineAt: '2030-02-01T00:00:00Z', createdAt: '2030-01-01T00:00:00Z', status: 'accepted', acceptedOptionId: 'chosen', options: [{ id: 'chosen', type: 'meeting', startAt: '2030-01-03T09:00:00Z', endAt: '2030-01-03T10:00:00Z' }] }
it('orders only selected confirmed meetings, not abandoned proposals or cancelled meetings', () => {
  const early = { ...base, id: 'early', options: [{ ...base.options[0], startAt: '2030-01-02T09:00:00Z', endAt: '2030-01-02T10:00:00Z' }] }
  const later = { ...base, id: 'later', options: [...base.options, { ...early.options[0], id: 'unselected' }] }
  const pending = { ...early, id: 'pending', status: 'suggested' }
  const cancelled = { ...early, id: 'cancelled', status: 'cancelled' }
  const malformed = { ...early, id: 'malformed', options: [early.options[0], early.options[0]] }
  const missing = { ...early, id: 'missing', options: [] }
  const source = [pending, later, cancelled, early, malformed, missing]
  expect(orderRequests(source, 'meeting').map(row => row.id)).toEqual(['early', 'later', 'cancelled', 'malformed', 'missing', 'pending'])
  expect(source[0]).toBe(pending)
})
