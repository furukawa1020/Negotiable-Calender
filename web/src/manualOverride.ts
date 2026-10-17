export function overrideCommand(date: Date, start: string, end: string, availability: string) {
  const time = /^(?:[01]\d|2[0-3]):[0-5]\d$/
  if (!time.test(start) || !time.test(end) || !['available', 'limited'].includes(availability) || !Number.isFinite(date.getTime())) throw new Error('invalid override')
  const from = new Date(date), to = new Date(date)
  from.setHours(Number(start.slice(0, 2)), Number(start.slice(3)), 0, 0)
  to.setHours(Number(end.slice(0, 2)), Number(end.slice(3)), 0, 0)
  if (to <= from) throw new Error('invalid override')
  return { startAt: from.toISOString(), endAt: to.toISOString(), expiresAt: new Date(to.getTime() + 86400000).toISOString(), state: {
    availability, interruptibility: availability === 'available' ? 'open' : 'urgent_only',
    requestability: availability === 'available' ? 'open' : 'later', reschedulability: availability === 'available' ? 'high' : 'low',
  } }
}
