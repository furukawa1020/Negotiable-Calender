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

type Props = {
  value: SharingPolicyDraft
  onChange: (value: SharingPolicyDraft) => void
  disabled?: boolean
}

const weekdays = [
  { value: 1, label: '月' }, { value: 2, label: '火' }, { value: 3, label: '水' },
  { value: 4, label: '木' }, { value: 5, label: '金' }, { value: 6, label: '土' },
  { value: 0, label: '日' },
]

const stateOptions = {
  availability: [
    ['available', '相談可能'], ['limited', '制限あり'], ['unavailable', '対応不可'], ['unknown', '要確認'],
  ],
  interruptibility: [
    ['open', 'いつでも可'], ['normal', '通常'], ['urgent_only', '緊急のみ'], ['do_not_interrupt', '割り込み不可'],
  ],
  requestability: [
    ['open', '依頼可'], ['async_only', '非同期のみ'], ['later', '後で'], ['closed', '受付停止'],
  ],
  reschedulability: [
    ['high', '変更しやすい'], ['medium', '変更可能'], ['low', '変更しにくい'], ['fixed', '固定'],
  ],
} as const

const stateLabels: Record<keyof InteractionState, string> = {
  availability: '空き状態',
  interruptibility: '割り込み',
  requestability: '依頼受付',
  reschedulability: '変更しやすさ',
}

const conditionLabels = { organization: '組織全体', calendar: 'カレンダー', event: '予定のbusy/free' }
const minutesToTime = (value: number) => `${String(Math.floor(value / 60)).padStart(2, '0')}:${String(value % 60).padStart(2, '0')}`
const timeToMinutes = (value: string) => {
  const [hour, minute] = value.split(':').map(Number)
  return hour * 60 + minute
}

const newPolicyRule = (): PolicyRuleDraft => ({
  conditionType: 'event',
  condition: { busyStatus: 'busy' },
  state: {
    availability: 'limited', interruptibility: 'urgent_only',
    requestability: 'async_only', reschedulability: 'low',
  },
  priority: 100,
  enabled: true,
})

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

export default function SharingPolicyEditor({ value, onChange, disabled = false }: Props) {
  const error = sharingPolicyError(value)
  const setDefault = (key: keyof InteractionState, next: string) => {
    onChange({ ...value, default: { ...value.default, [key]: next } })
  }
  const setWindow = (index: number, key: 'startMinute' | 'endMinute', next: string) => {
    onChange({ ...value, workingHours: value.workingHours.map((window, itemIndex) => itemIndex === index
      ? { ...window, [key]: timeToMinutes(next) }
      : window) })
  }
  const addWindow = (weekday: number) => {
    if (value.workingHours.length >= 14) return
    onChange({ ...value, workingHours: [...value.workingHours, { weekday, startMinute: 540, endMinute: 1080 }]
      .sort((left, right) => ((left.weekday + 6) % 7) - ((right.weekday + 6) % 7) || left.startMinute - right.startMinute) })
  }
  const removeWindow = (index: number) => onChange({
    ...value, workingHours: value.workingHours.filter((_, itemIndex) => itemIndex !== index),
  })
  const updateRule = (index: number, update: Partial<PolicyRuleDraft>) => onChange({
    ...value, rules: value.rules.map((rule, itemIndex) => itemIndex === index ? { ...rule, ...update } : rule),
  })
  const setRuleState = (index: number, key: keyof InteractionState, next: string) => updateRule(index, {
    state: { ...value.rules[index].state, [key]: next },
  })
  const moveRule = (index: number, offset: number) => {
    const destination = index + offset
    if (destination < 0 || destination >= value.rules.length) return
    const rules = [...value.rules]
    ;[rules[index], rules[destination]] = [rules[destination], rules[index]]
    onChange({ ...value, rules })
  }

  return (
    <div className="policy-editor" aria-busy={disabled}>
      <fieldset disabled={disabled}>
        <legend>基本の公開状態</legend>
        <div className="policy-state-grid">
          {(Object.keys(stateOptions) as Array<keyof InteractionState>).map((key) => (
            <label key={key}>{stateLabels[key]}
              <select aria-label={`基本の${stateLabels[key]}`} value={value.default[key]} onChange={(event) => setDefault(key, event.target.value)}>
                {stateOptions[key].map(([optionValue, label]) => <option key={optionValue} value={optionValue}>{label}</option>)}
              </select>
            </label>
          ))}
        </div>
      </fieldset>

      <fieldset disabled={disabled}>
        <legend>勤務時間</legend>
        <p className="field-help">曜日ごとに複数の時間帯を設定できます。未設定の曜日は勤務時間外として扱います。</p>
        <div className="working-hours-grid">
          {weekdays.map((day) => {
            const windows = value.workingHours.map((window, index) => ({ window, index })).filter(({ window }) => window.weekday === day.value)
            return (
              <div className="working-day" key={day.value}>
                <strong>{day.label}</strong>
                <div>
                  {windows.length === 0 ? <span className="empty-inline">休み</span> : windows.map(({ window, index }, order) => (
                    <span className="working-window" key={`${day.value}-${index}`}>
                      <input aria-label={`${day.label}曜日 勤務開始 ${order + 1}`} type="time" value={minutesToTime(window.startMinute)} onChange={(event) => setWindow(index, 'startMinute', event.target.value)} />
                      <span aria-hidden="true">—</span>
                      <input aria-label={`${day.label}曜日 勤務終了 ${order + 1}`} type="time" value={minutesToTime(window.endMinute)} onChange={(event) => setWindow(index, 'endMinute', event.target.value)} />
                      <button type="button" aria-label={`${day.label}曜日の時間帯${order + 1}を削除`} onClick={() => removeWindow(index)}>削除</button>
                    </span>
                  ))}
                </div>
                <button type="button" onClick={() => addWindow(day.value)} disabled={value.workingHours.length >= 14}>＋ 時間帯</button>
              </div>
            )
          })}
        </div>
      </fieldset>

      <fieldset disabled={disabled}>
        <legend>条件別ルール</legend>
        <p className="field-help">上にあるルールほど先に評価します。条件と公開状態だけを保存し、予定の詳細は保存しません。</p>
        {value.rules.length === 0 ? <p className="policy-empty">条件別ルールはありません。基本の公開状態を使用します。</p> : null}
        <ol className="policy-rule-list">
          {value.rules.map((rule, index) => (
            <li key={index} className={rule.enabled ? '' : 'disabled-rule'}>
              <div className="policy-rule-heading">
                <strong>ルール {index + 1}</strong>
                <label><input type="checkbox" checked={rule.enabled} onChange={(event) => updateRule(index, { enabled: event.target.checked })} />有効</label>
                <div className="rule-order-actions">
                  <button type="button" aria-label={`ルール${index + 1}を上へ`} disabled={index === 0} onClick={() => moveRule(index, -1)}>↑</button>
                  <button type="button" aria-label={`ルール${index + 1}を下へ`} disabled={index === value.rules.length - 1} onClick={() => moveRule(index, 1)}>↓</button>
                  <button type="button" aria-label={`ルール${index + 1}を削除`} onClick={() => onChange({ ...value, rules: value.rules.filter((_, itemIndex) => itemIndex !== index) })}>削除</button>
                </div>
              </div>
              <div className="policy-condition-grid">
                <label>条件
                  <select aria-label={`ルール${index + 1}の条件`} value={rule.conditionType} onChange={(event) => {
                    const conditionType = event.target.value as PolicyRuleDraft['conditionType']
                    const condition = conditionType === 'calendar' ? { calendarId: '' } : conditionType === 'event' ? { busyStatus: 'busy' as const } : {}
                    updateRule(index, { conditionType, condition })
                  }}>
                    {(Object.keys(conditionLabels) as Array<PolicyRuleDraft['conditionType']>).map((type) => <option key={type} value={type}>{conditionLabels[type]}</option>)}
                  </select>
                </label>
                {rule.conditionType === 'calendar' ? (
                  <label>Calendar ID<input aria-label={`ルール${index + 1}のCalendar ID`} maxLength={200} value={rule.condition.calendarId ?? ''} onChange={(event) => updateRule(index, { condition: { calendarId: event.target.value } })} /></label>
                ) : null}
                {rule.conditionType === 'event' ? (
                  <label>予定状態<select aria-label={`ルール${index + 1}の予定状態`} value={rule.condition.busyStatus ?? 'busy'} onChange={(event) => updateRule(index, { condition: { busyStatus: event.target.value as 'busy' | 'free' } })}><option value="busy">busy</option><option value="free">free</option></select></label>
                ) : null}
                <label>優先度<input aria-label={`ルール${index + 1}の優先度`} type="number" min="0" max="1000" step="1" value={rule.priority} onChange={(event) => updateRule(index, { priority: Number(event.target.value) })} /></label>
              </div>
              <div className="policy-state-grid compact">
                {(Object.keys(stateOptions) as Array<keyof InteractionState>).map((key) => (
                  <label key={key}>{stateLabels[key]}
                    <select aria-label={`ルール${index + 1}の${stateLabels[key]}`} value={rule.state[key]} onChange={(event) => setRuleState(index, key, event.target.value)}>
                      {stateOptions[key].map(([optionValue, label]) => <option key={optionValue} value={optionValue}>{label}</option>)}
                    </select>
                  </label>
                ))}
              </div>
            </li>
          ))}
        </ol>
        <button className="secondary-button add-policy-rule" type="button" disabled={value.rules.length >= 50} onClick={() => onChange({ ...value, rules: [...value.rules, newPolicyRule()] })}>＋ 条件別ルールを追加</button>
      </fieldset>

      <section className="policy-preview" aria-label="組織への公開プレビュー">
        <p className="eyebrow">PRIVACY PREVIEW</p>
        <strong>組織には次の調整状態だけが見えます</strong>
        <dl>{(Object.keys(stateOptions) as Array<keyof InteractionState>).map((key) => <div key={key}><dt>{stateLabels[key]}</dt><dd>{stateOptions[key].find(([optionValue]) => optionValue === value.default[key])?.[1]}</dd></div>)}</dl>
        <small>予定名・説明・場所・参加者・Calendar名は表示されません。</small>
      </section>
      {error ? <p className="policy-error" role="alert">{error}</p> : null}
    </div>
  )
}
