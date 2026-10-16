export function moveCalendarAnchor(current: Date, view: 'day' | 'week' | 'month', direction: -1 | 1): Date {
  const next = new Date(current)
  if (view === 'month') {
    // Set the day before changing month: JS otherwise rolls Jan 31 into March.
    const day = next.getDate()
    next.setDate(1)
    next.setMonth(next.getMonth() + direction)
    const lastDay = new Date(next.getFullYear(), next.getMonth() + 1, 0).getDate()
    next.setDate(Math.min(day, lastDay))
  } else {
    next.setDate(next.getDate() + direction * (view === 'week' ? 7 : 1))
  }
  return next
}
