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
