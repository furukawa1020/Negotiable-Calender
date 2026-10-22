import { useState, type ReactNode } from 'react'
import type { CoordinationRequest } from './bookingResponse'

const statuses = [
  ['pending', '受付待ち'], ['suggested', '候補提示済み'], ['accepted', '日時確定'],
  ['delegated', '担当変更済み'], ['async', '非同期回答済み'], ['declined', '辞退'],
  ['cancelled', 'キャンセル'], ['expired', '期限切れ'], ['completed', '完了'],
] as const
const normalize = (value: string) => value.normalize('NFKC').toLocaleLowerCase('ja-JP').trim()

export function filterRequests(requests: CoordinationRequest[], query: string, status: string): CoordinationRequest[] {
  const needle = normalize(query)
  return requests.filter(row => (status === '' || row.status === status)
    && [row.title, row.requesterUserId, row.targetUserId, row.id].some(value => normalize(value).includes(needle)))
}

export function RequestBrowser({ requests, children }: { requests: CoordinationRequest[]; children: (rows: CoordinationRequest[]) => ReactNode }) {
  const [query, setQuery] = useState('')
  const [status, setStatus] = useState('')
  const visible = filterRequests(requests, query, status)
  return <>
    <div className="request-tools" role="search" aria-label="依頼の絞り込み">
      <label>依頼を検索<input type="search" value={query} maxLength={200} placeholder="件名・相手・依頼ID" onChange={event => setQuery(event.target.value)} /></label>
      <label>状態<select value={status} onChange={event => setStatus(event.target.value)}>
        <option value="">すべての状態</option>
        {statuses.map(([value, label]) => <option key={value} value={value}>{label}</option>)}
      </select></label>
      <button type="button" className="secondary-button" disabled={!query && !status} onClick={() => { setQuery(''); setStatus('') }}>絞り込みを解除</button>
      <span role="status" aria-live="polite">{requests.length}件中 {visible.length}件を表示</span>
    </div>
    {requests.length > 0 && visible.length === 0 ? <p className="people-status">条件に一致する依頼はありません。検索語や状態を変更してください。</p> : null}
    {children(visible)}
  </>
}
