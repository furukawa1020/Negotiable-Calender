import { expect, it } from 'vitest'
import { readRequestList } from './requestList'

const meeting = { id: 'm', type: 'meeting', startAt: '2030-01-01T01:00:00Z', endAt: '2030-01-01T02:00:00Z' }
const request = { id: 'r', requesterUserId: 'a', targetUserId: 'b', title: 'Review', type: 'meeting', status: 'suggested', priority: 'normal', durationMinutes: 60, deadlineAt: '2030-01-02T00:00:00Z', createdAt: '2026-01-01T00:00:00Z', options: [meeting] }
it.each([
  { status: 'unknown' }, { type: 'unknown' }, { priority: 'unknown' }, { title: ' ' }, { targetUserId: 'a' },
  { options: [{ id: 'm', type: 'meeting' }] }, { options: [{ ...meeting, responseBy: meeting.startAt }] },
  { options: [{ ...meeting, requestId: 'foreign' }] }, { options: [{ ...meeting, type: 'async' }] },
  { acceptedOptionId: 'missing' }, { status: 'accepted' }, { status: 'completed' },
  { rescheduleProposal: { id: 'm', expectedOptionId: 'missing', proposerUserId: 'a', status: 'proposed' } },
])('rejects contradictory transport evidence %j', async patch => {
  await expect(readRequestList(Response.json({ requests: [{ ...request, ...patch }] }))).rejects.toThrow('invalid request list')
})
it.each(['accepted', 'completed', 'cancelled'])('preserves historical meeting evidence in %s requests', async status => {
  const value = { ...request, status, acceptedOptionId: 'm' }
  await expect(readRequestList(Response.json({ requests: [value] }))).resolves.toEqual([value])
})
it('accepts empty asynchronous choices without inventing a deadline', async () => {
  const value = { ...request, options: [{ id: 'a', type: 'async' }] }
  await expect(readRequestList(Response.json({ requests: [value] }))).resolves.toEqual([value])
})
