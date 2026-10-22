import { expect, it } from 'vitest'
import { readNotifications } from './notificationResponse'
const row = { id: 'n', type: 'request_received', requestId: 'r', message: 'New request', createdAt: '2030-01-01T00:00:00Z' }
it.each([null, {}, { notifications: null }, { notifications: [null] }, { notifications: [row, row] },
  ...[{ id: '' }, { message: {} }, { type: [] }, { requestId: null }, { createdAt: 'invalid' }, { readAt: 'invalid' }, { readAt: null }].map(patch => ({ notifications: [{ ...row, ...patch }] })),
])('rejects invalid notification rows %j', async value => {
  await expect(readNotifications(Response.json(value))).rejects.toThrow('invalid notifications')
})
it('accepts unread, read and empty snapshots', async () => {
  for (const notifications of [[], [row], [{ ...row, readAt: row.createdAt }]]) {
    await expect(readNotifications(Response.json({ notifications }))).resolves.toEqual({ notifications })
  }
})
