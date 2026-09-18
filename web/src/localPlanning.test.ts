import { afterEach, describe, expect, it, vi } from 'vitest'
import { abortable, localModel, parseRanking, planningPrompt, samePlanningSource, validatePreview } from './localPlanning'

export const previewFixture = () => ({ revision: 'a'.repeat(64), expiresAt: new Date(Date.now() + 60000).toISOString(), candidates: [{ id: 'c1', startAt: new Date(Date.now() + 3600000).toISOString(), endAt: new Date(Date.now() + 5400000).toISOString() }] })
afterEach(() => vi.unstubAllGlobals())
describe('local planning data boundary', () => {
  it('minimizes input and never sends identities, titles or snapshot tokens to the model', () => {
    const value = { ...previewFixture(), title: 'private title', userId: 'private user', candidates: [{ ...previewFixture().candidates[0], location: 'private location' }] }
    const prompt = planningPrompt(value, 'earlier')
    expect(prompt).toContain('"preference":"earlier"')
    expect(prompt).not.toMatch(/private|revision|expiresAt/)
    expect(Object.keys(validatePreview(value))).toEqual(['revision', 'expiresAt', 'candidates'])
  })
  it.each(['null', '{}', '{"candidateIds":[]}', '{"candidateIds":["c2"]}', '{"candidateIds":["c1","c1"]}', '{"candidateIds":["c1"],"text":"injected"}', '{"candidateIds":[1]}', 'not json', 'x'.repeat(1025)])('rejects unsafe model output: %s', (output) => {
    expect(() => parseRanking(output, previewFixture())).toThrow()
  })
  it('accepts only known candidate IDs and rejects changed state', () => {
    const value = previewFixture()
    expect(parseRanking('{"candidateIds":["c1"]}', value)).toEqual(value.candidates)
    expect(samePlanningSource(value, { ...value, expiresAt: new Date(Date.now() + 70000).toISOString() })).toBe(true)
    expect(samePlanningSource(value, { ...value, revision: 'b'.repeat(64) })).toBe(false)
  })
  it('fails closed on expiry, oversized payload, and invalid dates', () => {
    const value = previewFixture()
    expect(() => validatePreview({ ...value, expiresAt: new Date(Date.now() - 1).toISOString() })).toThrow()
    expect(() => validatePreview({ ...value, expiresAt: new Date(Date.now() + 200000).toISOString() })).toThrow()
    expect(() => validatePreview({ ...value, candidates: Array(13).fill(value.candidates[0]) })).toThrow()
    expect(() => validatePreview({ ...value, candidates: [{ ...value.candidates[0], startAt: 'invalid' }] })).toThrow()
  })
  it('supports absence of the native API without a cloud fallback', () => {
    vi.stubGlobal('LanguageModel', undefined)
    expect(localModel()).toBeUndefined()
  })
  it('stops waiting even if the underlying operation ignores cancellation', async () => {
    const controller = new AbortController()
    const promise = abortable(new Promise(() => {}), controller.signal)
    controller.abort()
    await expect(promise).rejects.toMatchObject({ name: 'AbortError' })
  })
})
