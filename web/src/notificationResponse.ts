export type AppNotification = {
  id: string
  type: string
  requestId: string
  message: string
  readAt?: string
  createdAt: string
}
const record = (value: unknown): value is Record<string, unknown> => !!value && typeof value === 'object' && !Array.isArray(value)
const text = (value: unknown) => typeof value === 'string' && value.trim().length > 0
const date = (value: unknown) => typeof value === 'string' && Number.isFinite(Date.parse(value))

export async function readNotifications(response: Response): Promise<{ notifications: AppNotification[] }> {
  const value: unknown = await response.json()
  if (!record(value) || !Array.isArray(value.notifications)) throw new Error('invalid notifications')
  const ids = new Set<string>()
  for (const row of value.notifications) {
    if (!record(row) || !['id', 'type', 'requestId', 'message'].every(key => text(row[key]))
      || !date(row.createdAt) || (row.readAt !== undefined && !date(row.readAt))
      || ids.has(String(row.id))) throw new Error('invalid notifications')
    ids.add(String(row.id))
  }
  return { notifications: value.notifications as AppNotification[] }
}
