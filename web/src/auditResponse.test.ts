import { expect, it } from 'vitest'
import { readAuditLogs } from './auditResponse'
const row = { id: 'a', organizationId: 'org', actorUserId: 'actor', action: 'request_accepted', resourceType: 'request', resourceId: 'r', createdAt: '2030-01-01T00:00:00Z' }
it.each([null, {}, { auditLogs: null }, { auditLogs: [null] }, { auditLogs: [row, row] },
  ...[{ id: '' }, { organizationId: [] }, { actorUserId: null }, { action: {} }, { resourceType: '' }, { resourceId: 7 }, { createdAt: 'invalid' }].map(patch => ({ auditLogs: [{ ...row, ...patch }] })),
])('rejects unrenderable audit rows %j', async value => {
  await expect(readAuditLogs(Response.json(value))).rejects.toThrow('invalid audit')
})
it('accepts empty and valid history without inventing an action allowlist', async () => {
  for (const auditLogs of [[], [row], [{ ...row, action: 'future_action' }]]) {
    await expect(readAuditLogs(Response.json({ auditLogs }))).resolves.toEqual({ auditLogs })
  }
})
