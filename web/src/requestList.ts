import type { CoordinationRequest } from './bookingResponse'

const record = (value: unknown): value is Record<string, unknown> => !!value && typeof value === 'object' && !Array.isArray(value)
const text = (value: unknown) => typeof value === 'string'

// This checks renderable shape, not authorization or booking eligibility.
// Mutation endpoints remain authoritative for participants and live conflicts.
export async function readRequestList(response: Response): Promise<CoordinationRequest[]> {
  const value: unknown = await response.json()
  if (!record(value) || !Array.isArray(value.requests)) throw new Error('invalid request list')
  const ids = new Set<string>()
  for (const item of value.requests) {
    if (!record(item) || typeof item.id !== 'string' || !item.id.trim() || ids.has(item.id)
      || !['requesterUserId', 'targetUserId', 'title', 'type', 'status', 'priority', 'deadlineAt', 'createdAt'].every(key => text(item[key]))
      || typeof item.durationMinutes !== 'number' || !Number.isFinite(item.durationMinutes)
      || !['asyncMessage', 'delegatedFromUserId', 'acceptedOptionId'].every(key => item[key] === undefined || text(item[key]))
      || !Array.isArray(item.options)
      || !item.options.every(option => record(option) && typeof option.id === 'string' && option.id.trim()
        && typeof option.type === 'string'
        && ['meeting', 'async', 'delegate', 'decline'].includes(String(option.type))
        && ['startAt', 'endAt', 'responseBy', 'proposedByUserId'].every(key => option[key] === undefined || text(option[key])))
      || (item.rescheduleProposal !== undefined && (!record(item.rescheduleProposal)
        || !['id', 'expectedOptionId', 'proposerUserId', 'status'].every(key => text((item.rescheduleProposal as Record<string, unknown>)[key]))))) {
      throw new Error('invalid request list')
    }
    ids.add(item.id)
  }
  return value.requests as CoordinationRequest[]
}
