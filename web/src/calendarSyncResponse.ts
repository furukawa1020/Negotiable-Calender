export async function readCalendarSync(response: Response): Promise<{ busySpanCount: number; lastSyncedAt: string }> {
  const value = await response.json()
  if (!value || value.synced !== true || !Number.isSafeInteger(value.busySpanCount) || value.busySpanCount < 0
    || typeof value.lastSyncedAt !== 'string' || !Number.isFinite(Date.parse(value.lastSyncedAt))
    || Date.parse(value.lastSyncedAt) > Date.now() + 60_000) throw new Error('sync acknowledgement invalid')
  return { busySpanCount: value.busySpanCount, lastSyncedAt: value.lastSyncedAt }
}
