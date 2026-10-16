export type InteractionState = {
  availability: 'available' | 'limited' | 'unavailable' | 'unknown'
  interruptibility: 'open' | 'normal' | 'urgent_only' | 'do_not_interrupt'
  requestability: 'open' | 'async_only' | 'later' | 'closed'
  reschedulability: 'high' | 'medium' | 'low' | 'fixed'
}

export type PolicyRuleDraft = {
  conditionType: 'organization' | 'calendar' | 'event'
  condition: { calendarId?: string; busyStatus?: 'busy' | 'free' }
  state: InteractionState
  priority: number
  enabled: boolean
}

export type SharingPolicyDraft = {
  default: InteractionState
  workingHours: Array<{ weekday: number; startMinute: number; endMinute: number }>
  rules: PolicyRuleDraft[]
}

export const sharingPolicyError = (value: SharingPolicyDraft) => {
  if (value.workingHours.length > 14) return '勤務時間帯は14件以内にしてください。'
  if (value.rules.length > 50) return 'ルールは50件以内にしてください。'
  for (const window of value.workingHours) {
    if (!Number.isInteger(window.weekday) || window.weekday < 0 || window.weekday > 6
      || !Number.isInteger(window.startMinute) || !Number.isInteger(window.endMinute)
      || window.startMinute < 0 || window.endMinute > 1440 || window.endMinute <= window.startMinute) {
      return '勤務時間の開始と終了を確認してください。'
    }
    const overlap = value.workingHours.some((other) => other !== window && other.weekday === window.weekday
      && window.startMinute < other.endMinute && other.startMinute < window.endMinute)
    if (overlap) return '同じ曜日の勤務時間帯が重複しています。'
  }
  for (const rule of value.rules) {
    if (!Number.isInteger(rule.priority) || rule.priority < 0 || rule.priority > 1000) return '優先度は0〜1000の整数で指定してください。'
    if (rule.conditionType === 'calendar') {
      const calendarId = rule.condition.calendarId?.trim() ?? ''
      if (!calendarId) return 'Calendar IDを入力してください。'
      if (calendarId.length > 200) return 'Calendar IDは200文字以内にしてください。'
    }
  }
  return ''
}

const record = (value: unknown): value is Record<string, unknown> => !!value && typeof value === 'object' && !Array.isArray(value)
function validState(value: unknown): value is InteractionState {
  if (!record(value)) return false
  if (!['availability', 'interruptibility', 'requestability', 'reschedulability'].every(key => typeof value[key] === 'string')) return false
  return ['available', 'limited', 'unavailable', 'unknown'].includes(String(value.availability))
    && ['open', 'normal', 'urgent_only', 'do_not_interrupt'].includes(String(value.interruptibility))
    && ['open', 'async_only', 'later', 'closed'].includes(String(value.requestability))
    && ['high', 'medium', 'low', 'fixed'].includes(String(value.reschedulability))
}

// Empty arrays (and Go's nil slices encoded as null) mean no windows/rules,
// never permission to silently substitute the application's initial defaults.
export function parseSharingPolicy(value: unknown): SharingPolicyDraft {
  const invalid = () => new Error('invalid sharing policy')
  if (!record(value) || !validState(value.default)) throw invalid()
  const hours = value.workingHours === null ? [] : value.workingHours
  const rules = value.rules === null ? [] : value.rules
  if (!Array.isArray(hours) || !Array.isArray(rules)) throw invalid()
  if (!hours.every(w => record(w) && Number.isInteger(w.weekday) && Number.isInteger(w.startMinute) && Number.isInteger(w.endMinute))) throw invalid()
  if (!rules.every(r => record(r) && validState(r.state) && record(r.condition)
    && typeof r.conditionType === 'string'
    && ['organization', 'calendar', 'event'].includes(String(r.conditionType))
    && typeof r.enabled === 'boolean' && Number.isInteger(r.priority)
    && (r.conditionType !== 'calendar' || typeof r.condition.calendarId === 'string')
    && (r.conditionType !== 'event' || (typeof r.condition.busyStatus === 'string' && ['busy', 'free'].includes(r.condition.busyStatus))))) throw invalid()
  const draft: SharingPolicyDraft = {
    default: value.default,
    workingHours: hours.map(({ weekday, startMinute, endMinute }) => ({ weekday, startMinute, endMinute })),
    rules: rules.map(({ conditionType, condition, state, priority, enabled }) => ({ conditionType, condition, state, priority, enabled })),
  }
  if (sharingPolicyError(draft)) throw invalid()
  return draft
}
