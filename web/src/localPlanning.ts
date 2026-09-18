export type PlanningCandidate = { id: string; startAt: string; endAt: string }
export class PlanningRateLimitError extends Error {
  readonly retryAfter: number
  constructor(header: string | null) {
    super('planning temporarily limited')
    const seconds = Number(header ?? '60')
    this.retryAfter = Number.isFinite(seconds) && seconds > 0 ? Math.min(60, Math.ceil(seconds)) : 60
  }
}
export const planningFailureMessage = (error: unknown, fallback: string) => error instanceof PlanningRateLimitError
  ? `候補確認の回数上限に達しました。約 ${error.retryAfter} 秒待ってから、閉じて再試行してください。`
  : fallback
export type PlanningPreview = { revision: string; expiresAt: string; candidates: PlanningCandidate[] }
export type PlanningPreference = 'earlier' | 'later'
export type LocalAvailability = 'unavailable' | 'downloadable' | 'downloading' | 'available'
type LanguageOptions = { expectedInputs: Array<{ type: 'text'; languages: string[] }>; expectedOutputs: Array<{ type: 'text'; languages: string[] }> }
export type LocalSession = { prompt: (input: string, options: { signal: AbortSignal; responseConstraint: object }) => Promise<string>; destroy: () => void }
export type LocalModel = {
  availability: (options: LanguageOptions) => Promise<LocalAvailability>
  create: (options: LanguageOptions & { signal: AbortSignal }) => Promise<LocalSession>
}
export const languageOptions: LanguageOptions = { expectedInputs: [{ type: 'text', languages: ['en'] }], expectedOutputs: [{ type: 'text', languages: ['en'] }] }

// Chrome's on-device Prompt API only. No SDK, network model client, API key,
// cloud fallback or sampling experiment is present in this module.
export const localModel = (): LocalModel | undefined => (globalThis as typeof globalThis & { LanguageModel?: LocalModel }).LanguageModel

// Bound waiting even when a browser implementation ignores AbortSignal.
export function abortable<T>(promise: Promise<T>, signal: AbortSignal): Promise<T> {
  return new Promise((resolve, reject) => {
    const cancel = () => reject(new DOMException('Aborted', 'AbortError'))
    if (signal.aborted) cancel()
    else signal.addEventListener('abort', cancel, { once: true })
    promise.then(resolve, reject).finally(() => signal.removeEventListener('abort', cancel))
  })
}

export function validatePreview(value: unknown, now = Date.now()): PlanningPreview {
  if (!value || typeof value !== 'object') throw new Error('invalid preview')
  const v = value as PlanningPreview
  const expiry = Date.parse(v.expiresAt)
  if (typeof v.revision !== 'string' || !/^[a-f0-9]{64}$/.test(v.revision) || !Number.isFinite(expiry) || expiry <= now || expiry > now + 121000 || !Array.isArray(v.candidates) || v.candidates.length < 1 || v.candidates.length > 12) throw new Error('stale preview')
  const candidates = v.candidates.map((c, i) => {
    if (!c || c.id !== `c${i + 1}` || typeof c.startAt !== 'string' || typeof c.endAt !== 'string' || !Number.isFinite(Date.parse(c.startAt)) || !Number.isFinite(Date.parse(c.endAt)) || Date.parse(c.startAt) <= now || Date.parse(c.endAt) <= Date.parse(c.startAt)) throw new Error('invalid candidate')
    return { id: c.id, startAt: c.startAt, endAt: c.endAt }
  })
  return { revision: v.revision, expiresAt: v.expiresAt, candidates }
}

export function planningPrompt(preview: PlanningPreview, preference: PlanningPreference): string {
  const v = validatePreview(preview)
  if (preference !== 'earlier' && preference !== 'later') throw new Error('invalid preference')
  return 'Compare the candidate times using the preference. Return only JSON {"candidateIds":[...]} with one to three distinct supplied IDs in preferred order. Never invent IDs or book anything.\n' + JSON.stringify({ preference, candidates: v.candidates })
}

export function parseRanking(raw: string, preview: PlanningPreview): PlanningCandidate[] {
  validatePreview(preview)
  if (typeof raw !== 'string' || raw.length > 1024) throw new Error('invalid AI output')
  const value = JSON.parse(raw) as { candidateIds?: unknown }
  if (!value || Object.keys(value).join(',') !== 'candidateIds' || !Array.isArray(value.candidateIds) || value.candidateIds.length < 1 || value.candidateIds.length > 3 || new Set(value.candidateIds).size !== value.candidateIds.length) throw new Error('invalid AI output')
  return value.candidateIds.map((id) => {
    const candidate = preview.candidates.find((c) => c.id === id)
    if (!candidate) throw new Error('unknown candidate')
    return candidate
  })
}

export function samePlanningSource(before: PlanningPreview, after: PlanningPreview): boolean {
  validatePreview(before); validatePreview(after)
  return before.revision === after.revision && JSON.stringify(before.candidates) === JSON.stringify(after.candidates)
}

export const rankingSchema = (preview: PlanningPreview) => ({ type: 'object', properties: { candidateIds: { type: 'array', items: { type: 'string', enum: preview.candidates.map((c) => c.id) }, minItems: 1, maxItems: 3, uniqueItems: true } }, required: ['candidateIds'], additionalProperties: false })
