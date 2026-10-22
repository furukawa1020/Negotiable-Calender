export type AuditEvent = {
  id: string
  organizationId: string
  actorUserId: string
  action: string
  resourceType: string
  resourceId: string
  createdAt: string
}
const record = (value: unknown): value is Record<string, unknown> => !!value && typeof value === 'object' && !Array.isArray(value)

export async function readAuditLogs(response: Response): Promise<{ auditLogs: AuditEvent[] }> {
  const value: unknown = await response.json()
  if (!record(value) || !Array.isArray(value.auditLogs)) throw new Error('invalid audit')
  const ids = new Set<string>()
  for (const row of value.auditLogs) {
    if (!record(row) || !['id', 'organizationId', 'actorUserId', 'action', 'resourceType', 'resourceId'].every(key => typeof row[key] === 'string' && row[key].trim())
      || typeof row.createdAt !== 'string' || !Number.isFinite(Date.parse(row.createdAt))
      || ids.has(String(row.id))) throw new Error('invalid audit')
    ids.add(String(row.id))
  }
  return { auditLogs: value.auditLogs as AuditEvent[] }
}
