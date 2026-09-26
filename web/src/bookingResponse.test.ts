import { describe, expect, it } from 'vitest'
import { matchesRescheduleOutcome, readAcceptance, readRescheduleSnapshot } from './bookingResponse'

function snapshot() {
  return {
    id: 'booking', organizationId: 'org', requesterUserId: 'alice', targetUserId: 'bob',
    title: 'Synthetic meeting', type: 'meeting', durationMinutes: 30, priority: 'normal',
    status: 'accepted', acceptedOptionId: 'old', deadlineAt: '2099-02-01T00:00:00Z', createdAt: '2099-01-01T00:00:00Z',
    options: [
      { id: 'old', requestId: 'booking', type: 'meeting', startAt: '2099-01-02T00:00:00Z', endAt: '2099-01-02T00:30:00Z' },
      { id: 'new', requestId: 'booking', type: 'meeting', startAt: '2099-01-03T00:00:00Z', endAt: '2099-01-03T00:30:00Z' },
    ],
    rescheduleProposal: { id: 'new', expectedOptionId: 'old', proposerUserId: 'alice', status: 'proposed' },
  }
}
const read = (value: unknown, actor = 'bob') => readRescheduleSnapshot(Response.json(value), 'booking', 'org', actor)
const invalidSnapshots: [string, (value: ReturnType<typeof snapshot>) => unknown][] = [
  ['null', () => null], ['array', () => []], ['missing fields', () => ({})],
  ['wrong request', v => ({ ...v, id: 'another' })], ['wrong workspace', v => ({ ...v, organizationId: 'another' })],
  ['missing participant', v => ({ ...v, requesterUserId: '' })], ['same participants', v => ({ ...v, requesterUserId: 'bob' })],
  ['nonparticipant', v => ({ ...v, targetUserId: 'outsider' })],
  ['object title', v => ({ ...v, title: {} })], ['bad duration', v => ({ ...v, durationMinutes: '30' })],
  ['negative duration', v => ({ ...v, durationMinutes: -1 })], ['invalid deadline', v => ({ ...v, deadlineAt: 'invalid' })],
  ['invalid creation', v => ({ ...v, createdAt: {} })], ['object message', v => ({ ...v, asyncMessage: {} })],
  ['unknown status', v => ({ ...v, status: 'unknown' })], ['coerced status', v => ({ ...v, status: ['accepted'] })],
  ['missing options', v => ({ ...v, options: null })], ['null option', v => ({ ...v, options: [null] })],
  ['duplicate option', v => ({ ...v, options: [v.options[0], v.options[0]] })],
  ['wrong option owner', v => ({ ...v, options: [{ ...v.options[0], requestId: 'another' }, v.options[1]] })],
  ['coerced option type', v => ({ ...v, options: [{ ...v.options[0], type: ['meeting'] }, v.options[1]] })],
  ['invalid option date', v => ({ ...v, options: [{ ...v.options[0], startAt: 'invalid' }, v.options[1]] })],
  ['inverted option', v => ({ ...v, options: [{ ...v.options[0], endAt: v.options[0].startAt }, v.options[1]] })],
  ['invalid response date', v => ({ ...v, options: [{ ...v.options[0], responseBy: {} }, v.options[1]] })],
  ['missing chosen option', v => ({ ...v, acceptedOptionId: 'missing' })],
  ['nonmeeting chosen option', v => ({ ...v, options: [{ ...v.options[0], type: 'async' }, v.options[1]] })],
  ['null proposal', v => ({ ...v, rescheduleProposal: null })],
  ['bad proposal status', v => ({ ...v, rescheduleProposal: { ...v.rescheduleProposal, status: 'unknown' } })],
  ['bad proposer', v => ({ ...v, rescheduleProposal: { ...v.rescheduleProposal, proposerUserId: ['alice'] } })],
  ['missing proposal option', v => ({ ...v, rescheduleProposal: { ...v.rescheduleProposal, id: 'absent' } })],
]

describe('booking response contracts', () => {
  it.each(invalidSnapshots)('rejects %s without returning a renderable request', async (_, change) => {
    await expect(read(change(snapshot()))).rejects.toThrow('unconfirmed')
  })
  it.each(['alice', 'bob'])('accepts the full snapshot for participant %s', async actor => {
    expect(await read(snapshot(), actor)).toEqual(snapshot())
  })
  it.each(['cancelled', 'completed'])('accepts a newer %s snapshot without claiming reschedule success', async status => {
    const value = await read({ ...snapshot(), status })
    expect(value.status).toBe(status)
    expect(matchesRescheduleOutcome(value, { action: 'accept', proposalId: 'new', expectedOptionId: 'old' })).toBe(false)
  })
  it.each(['propose', 'accept', 'decline', 'withdraw'] as const)('recognizes the exact %s outcome only', async action => {
    const statuses = { propose: 'proposed', accept: 'accepted', decline: 'declined', withdraw: 'withdrawn' }
    const v = snapshot()
    const value = await read({ ...v, acceptedOptionId: action === 'accept' ? 'new' : 'old', rescheduleProposal: { ...v.rescheduleProposal, status: statuses[action] } })
    const command = { action, proposalId: 'new', expectedOptionId: 'old' }
    expect(matchesRescheduleOutcome(value, command)).toBe(true)
    expect(matchesRescheduleOutcome(value, { ...command, proposalId: 'other' })).toBe(false)
    expect(matchesRescheduleOutcome(value, { ...command, expectedOptionId: 'other' })).toBe(false)
  })
  it.each([null, {}, { id: 'other', status: 'accepted', acceptedOptionId: 'old' }, { id: 'booking', status: 'cancelled', acceptedOptionId: 'old' }, { id: 'booking', status: 'accepted', acceptedOptionId: 'other' }])('rejects invalid acceptance %j', async value => {
    await expect(readAcceptance(Response.json(value), 'booking', 'old')).rejects.toThrow('invalid')
  })
  it('accepts an exact acknowledgement including an idempotent replay', async () => {
    expect(await readAcceptance(Response.json({ id: 'booking', status: 'accepted', acceptedOptionId: 'old' }, { headers: { 'Idempotency-Replayed': 'true' } }), 'booking', 'old')).toBe(true)
  })
  it('rejects empty or malformed success bodies', async () => {
    await expect(readAcceptance(new Response(null, { status: 204 }), 'booking', 'old')).rejects.toThrow()
    await expect(readRescheduleSnapshot(new Response('invalid'), 'booking', 'org', 'bob')).rejects.toThrow()
  })
})
