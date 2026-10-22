import type { CoordinationRequest } from './bookingResponse'

const record = (value: unknown): value is Record<string, unknown> => !!value && typeof value === 'object' && !Array.isArray(value)
const text = (value: unknown) => typeof value === 'string'
const date = (value: unknown) => typeof value === 'string' && Number.isFinite(Date.parse(value))
const nonblank = (value: unknown) => typeof value === 'string' && value.trim().length > 0
const choice = (value: unknown, allowed: string[]) => typeof value === 'string' && allowed.includes(value)

// This checks renderable shape, not authorization or booking eligibility.
// Mutation endpoints remain authoritative for participants and live conflicts.
export async function readRequestList(response: Response): Promise<CoordinationRequest[]> {
  const value: unknown = await response.json()
  if (!record(value) || !Array.isArray(value.requests)) throw new Error('invalid request list')
  const ids = new Set<string>()
  for (const item of value.requests) {
    if (!record(item) || typeof item.id !== 'string' || !item.id.trim() || ids.has(item.id)
      || !['requesterUserId', 'targetUserId', 'title', 'type', 'status', 'priority', 'deadlineAt', 'createdAt'].every(key => text(item[key]))
      || !['requesterUserId', 'targetUserId', 'title'].every(key => nonblank(item[key]))
      || item.requesterUserId === item.targetUserId
      || !choice(item.status, ['pending', 'suggested', 'accepted', 'declined', 'delegated', 'cancelled', 'expired', 'completed', 'async'])
      || !choice(item.type, ['quick_question', 'meeting', 'review', 'approval', 'decision', 'async_response', 'urgent_contact'])
      || !choice(item.priority, ['low', 'normal', 'high', 'urgent'])
      || !date(item.deadlineAt) || !date(item.createdAt)
      || typeof item.durationMinutes !== 'number' || !Number.isSafeInteger(item.durationMinutes) || item.durationMinutes <= 0
      || !['asyncMessage', 'delegatedFromUserId', 'acceptedOptionId'].every(key => item[key] === undefined || text(item[key]))
      || !Array.isArray(item.options)
      || !item.options.every(option => record(option) && typeof option.id === 'string' && option.id.trim()
        && typeof option.type === 'string'
        && ['meeting', 'async', 'delegate', 'decline'].includes(String(option.type))
        && ['startAt', 'endAt', 'responseBy'].every(key => option[key] === undefined || date(option[key]))
        && (option.proposedByUserId === undefined || text(option.proposedByUserId))
        && (option.requestId === undefined || option.requestId === item.id)
        && (option.type === 'meeting' ? date(option.startAt) && date(option.endAt) : option.startAt === undefined && option.endAt === undefined)
        && (option.type === 'async' || option.responseBy === undefined)
        && (option.type === 'delegate' || option.delegateUserId === undefined)
        && (option.startAt === undefined || option.endAt === undefined || Date.parse(String(option.endAt)) > Date.parse(String(option.startAt))))
      || new Set(item.options.map(option => option.id)).size !== item.options.length
      || (item.rescheduleProposal !== undefined && (!record(item.rescheduleProposal)
        || !['id', 'expectedOptionId', 'proposerUserId', 'status'].every(key => text((item.rescheduleProposal as Record<string, unknown>)[key]))))) {
      throw new Error('invalid request list')
    }
    if (item.acceptedOptionId && !item.options.some(option => option.id === item.acceptedOptionId && option.type === 'meeting')) throw new Error('invalid request list')
    if (['accepted', 'completed'].includes(item.status as string) && !nonblank(item.acceptedOptionId)) throw new Error('invalid request list')
    const proposal = item.rescheduleProposal, options = item.options
    if (record(proposal) && (!nonblank(proposal.id) || !nonblank(proposal.expectedOptionId)
      || proposal.id === proposal.expectedOptionId
      || ![item.requesterUserId, item.targetUserId].includes(proposal.proposerUserId)
      || !choice(proposal.status, ['proposed', 'accepted', 'declined', 'withdrawn'])
      || ![proposal.id, proposal.expectedOptionId].every(id => options.some(option => option.id === id && option.type === 'meeting')))) throw new Error('invalid request list')
    ids.add(item.id)
  }
  return value.requests as CoordinationRequest[]
}
