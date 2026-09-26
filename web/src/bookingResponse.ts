import type { RescheduleCommand, RescheduleProposal } from './RescheduleMeeting'

export type CoordinationOption = {
  id: string
  type: 'meeting' | 'async' | 'delegate' | 'decline'
  startAt?: string
  endAt?: string
  responseBy?: string
  proposedByUserId?: string
}
export type CoordinationRequest = {
  id: string
  requesterUserId: string
  targetUserId: string
  title: string
  type: string
  durationMinutes: number
  deadlineAt: string
  priority: string
  status: string
  asyncMessage?: string
  delegatedFromUserId?: string
  acceptedOptionId?: string
  rescheduleProposal?: RescheduleProposal
  options: CoordinationOption[]
  createdAt: string
}

const record = (value: unknown): value is Record<string, unknown> => value !== null && typeof value === 'object' && !Array.isArray(value)
const text = (value: unknown): value is string => typeof value === 'string' && value.trim().length > 0
const date = (value: unknown): value is string => typeof value === 'string' && Number.isFinite(Date.parse(value))
const optionalText = (value: unknown) => value === undefined || typeof value === 'string'
const choice = (value: unknown, allowed: string[]) => typeof value === 'string' && allowed.includes(value)

export async function readAcceptance(response: Response, id: string, optionID: string): Promise<true> {
  const value: unknown = await response.json()
  if (!text(id) || !text(optionID) || !record(value) || value.id !== id || value.status !== 'accepted' || value.acceptedOptionId !== optionID) {
    throw new Error('acceptance acknowledgement invalid')
  }
  return true
}

export async function readRescheduleSnapshot(response: Response, id: string, organizationID: string, actor: string): Promise<CoordinationRequest> {
  const value: unknown = await response.json()
  const invalid = () => new Error('reschedule snapshot unconfirmed')
  if (!text(id) || !text(organizationID) || !text(actor) || !record(value) || value.id !== id || value.organizationId !== organizationID
    || !text(value.requesterUserId) || !text(value.targetUserId) || value.requesterUserId === value.targetUserId
    || ![value.requesterUserId, value.targetUserId].includes(actor)
    || !text(value.title) || !text(value.type) || !text(value.priority)
    || !Number.isInteger(value.durationMinutes) || (value.durationMinutes as number) <= 0
    || !date(value.deadlineAt) || !date(value.createdAt)
    || !choice(value.status, ['pending', 'suggested', 'accepted', 'declined', 'delegated', 'cancelled', 'expired', 'completed', 'async'])
    || !optionalText(value.acceptedOptionId) || !optionalText(value.asyncMessage) || !optionalText(value.delegatedFromUserId)
    || !Array.isArray(value.options)) throw invalid()

  const ids = new Set<string>()
  for (const option of value.options) {
    if (!record(option) || !text(option.id) || ids.has(option.id)
      || !choice(option.type, ['meeting', 'async', 'delegate', 'decline'])
      || (option.requestId !== undefined && option.requestId !== id)
      || !optionalText(option.proposedByUserId)
      || (option.startAt !== undefined && !date(option.startAt))
      || (option.endAt !== undefined && !date(option.endAt))
      || (option.responseBy !== undefined && !date(option.responseBy))) throw invalid()
    if (option.type === 'meeting' && (!date(option.startAt) || !date(option.endAt)
      || Date.parse(option.endAt) <= Date.parse(option.startAt))) throw invalid()
    ids.add(option.id)
  }
  const options = value.options as CoordinationOption[]
  const meeting = (optionID: unknown) => options.some(option => option.id === optionID && option.type === 'meeting')
  if (value.status === 'accepted' && !meeting(value.acceptedOptionId)) throw invalid()
  const proposal = value.rescheduleProposal
  if (proposal !== undefined && (!record(proposal) || !text(proposal.id) || !text(proposal.expectedOptionId)
    || proposal.id === proposal.expectedOptionId
    || !text(proposal.proposerUserId) || ![value.requesterUserId, value.targetUserId].includes(proposal.proposerUserId)
    || !choice(proposal.status, ['proposed', 'accepted', 'declined', 'withdrawn'])
    || !meeting(proposal.id) || !meeting(proposal.expectedOptionId))) throw invalid()
  return value as CoordinationRequest
}

// The API reads the request after committing. A valid snapshot can reflect a
// newer operation, so do not reject it or announce our older command as current.
export function matchesRescheduleOutcome(value: CoordinationRequest, command: RescheduleCommand): boolean {
  const proposal = value.rescheduleProposal
  const statuses = { propose: 'proposed', accept: 'accepted', decline: 'declined', withdraw: 'withdrawn' }
  return value.status === 'accepted' && proposal?.id === command.proposalId
    && proposal.expectedOptionId === command.expectedOptionId && proposal.status === statuses[command.action]
    && value.acceptedOptionId === (command.action === 'accept' ? command.proposalId : command.expectedOptionId)
    && (command.action !== 'propose' || Date.parse(command.startAt ?? '') === Date.parse(value.options.find(option => option.id === command.proposalId)?.startAt ?? ''))
}
