import { useState, type ReactNode } from 'react'
import type { CoordinationRequest } from './bookingResponse'

const statuses = [
  ['pending', '受付待ち'], ['suggested', '候補提示済み'], ['accepted', '日時確定'],
  ['delegated', '担当変更済み'], ['async', '非同期回答済み'], ['declined', '辞退'],
  ['cancelled', 'キャンセル'], ['expired', '期限切れ'], ['completed', '完了'],
] as const
const normalize = (value: string) => value.normalize('NFKC').toLocaleLowerCase('ja-JP').trim()
export type RequestOrder = 'newest' | 'deadline' | 'priority'
const priorities: Record<string, number> = { urgent: 3, high: 2, normal: 1, low: 0 }
export function orderRequests(requests: CoordinationRequest[], order: RequestOrder): CoordinationRequest[] {
  return [...requests].sort((a, b) => {
    const deadline = Date.parse(a.deadlineAt) - Date.parse(b.deadlineAt)
    const primary = order === 'deadline' ? deadline : order === 'priority'
      ? (priorities[b.priority] ?? -1) - (priorities[a.priority] ?? -1)
      : Date.parse(b.createdAt) - Date.parse(a.createdAt)
    return primary || deadline || (a.id < b.id ? -1 : a.id > b.id ? 1 : 0)
  })
}

export function filterRequests(requests: CoordinationRequest[], query: string, status: string): CoordinationRequest[] {
  const needle = normalize(query)
  return requests.filter(row => (status === '' || row.status === status)
    && [row.title, row.requesterUserId, row.targetUserId, row.id].some(value => normalize(value).includes(needle)))
}

export function RequestBrowser({ requests, children }: { requests: CoordinationRequest[]; children: (rows: CoordinationRequest[]) => ReactNode }) {
  const [query, setQuery] = useState('')
  const [status, setStatus] = useState('')
  const [order, setOrder] = useState<RequestOrder>('newest')
  const visible = orderRequests(filterRequests(requests, query, status), order)
  return <>
    <div className="request-tools" role="search" aria-label="依頼の絞り込み">
      <label>依頼を検索<input type="search" value={query} maxLength={200} placeholder="件名・相手・依頼ID" onChange={event => setQuery(event.target.value)} /></label>
      <label>状態<select value={status} onChange={event => setStatus(event.target.value)}>
        <option value="">すべての状態</option>
        {statuses.map(([value, label]) => <option key={value} value={value}>{label}</option>)}
      </select></label>
      <label>並び順<select value={order} onChange={event => setOrder(event.target.value as RequestOrder)}>
        <option value="newest">作成が新しい順</option>
        <option value="deadline">期限が近い順</option>
        <option value="priority">優先度が高い順</option>
      </select></label>
      <button type="button" className="secondary-button" disabled={!query && !status} onClick={() => { setQuery(''); setStatus('') }}>絞り込みを解除</button>
      <span className="request-count" aria-live="polite">{requests.length}件中 {visible.length}件を表示</span>
    </div>
    {requests.length > 0 && visible.length === 0 ? <p className="people-status">条件に一致する依頼はありません。検索語や状態を変更してください。</p> : null}
    {children(visible)}
  </>
}
