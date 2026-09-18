import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { LocalPlanning } from './LocalPlanningPanel'
import { PlanningRateLimitError, planningFailureMessage } from './localPlanning'

afterEach(cleanup)
it('shows a bounded retry delay rather than telling users to resync on a quota rejection', async () => {
  const load = vi.fn().mockRejectedValue(new PlanningRateLimitError('42'))
  render(<LocalPlanning loadPreview={load} />)
  fireEvent.click(screen.getByRole('button', { name: '端末内 AI で候補を比べる' }))
  expect(await screen.findByRole('alert')).toHaveTextContent('約 42 秒')
  expect(screen.queryByRole('checkbox')).not.toBeInTheDocument()
  expect(load).toHaveBeenCalledTimes(1)
})
it.each([null, 'invalid', 'Infinity', '-1', '9000'])('does not echo untrusted Retry-After values: %s', (raw) => {
  expect(new PlanningRateLimitError(raw).retryAfter).toBe(60)
  expect(planningFailureMessage(new PlanningRateLimitError(raw), 'fallback')).toContain('約 60 秒')
})
it('never exposes internal errors in planning UI', () => {
  expect(planningFailureMessage(new Error('private server detail'), 'safe fallback')).toBe('safe fallback')
})
