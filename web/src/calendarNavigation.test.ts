import { expect, it } from 'vitest'
import { moveCalendarAnchor } from './calendarNavigation'

it.each([
  [2026, 0, 31, 1, 2026, 1, 28],
  [2028, 0, 31, 1, 2028, 1, 29],
  [2026, 2, 31, -1, 2026, 1, 28],
  [2028, 2, 31, -1, 2028, 1, 29],
  [2026, 4, 31, -1, 2026, 3, 30],
  [2026, 11, 31, 1, 2027, 0, 31],
  [2026, 0, 31, -1, 2025, 11, 31],
] as const)('moves %i/%i/%i by %i month to %i/%i/%i', (y, m, d, direction, ey, em, ed) => {
  const original = new Date(y, m, d, 12, 34)
  const value = moveCalendarAnchor(original, 'month', direction)
  expect(value).toEqual(new Date(ey, em, ed, 12, 34))
  expect(original).toEqual(new Date(y, m, d, 12, 34))
})

it('preserves day/week navigation across year boundaries', () => {
  expect(moveCalendarAnchor(new Date(2026, 11, 31), 'day', 1)).toEqual(new Date(2027, 0, 1))
  expect(moveCalendarAnchor(new Date(2027, 0, 1), 'week', -1)).toEqual(new Date(2026, 11, 25))
})
