import { useState, type ReactNode } from 'react'
import type { CoordinationRequest } from './bookingResponse'
import { filterRequests, orderRequests, type RequestOrder } from './requestBrowserModel'

const statuses = [
  ['pending', '受付待ち'], ['suggested', '候補提示済み'], ['accepted', '日時確定'],
  ['delegated', '担当変更済み'], ['async', '非同期回答済み'], ['declined', '辞退'],
  ['cancelled', 'キャンセル'], ['expired', '期限切れ'], ['completed', '完了'],
] as const
export function RequestBrowser({ requests, children }: { requests: CoordinationRequest[]; children: (rows: CoordinationRequest[]) => ReactNode }) {
  const [query, setQuery] = useState('')
  const [status, setStatus] = useState('')
  const [order, setOrder] = useState<RequestOrder>('newest')
  const [page, setPage] = useState(0)
  const visible = orderRequests(filterRequests(requests, query, status), order)
  const pages = Math.max(1, Math.ceil(visible.length / 20))
  const currentPage = Math.min(page, pages - 1)
  // Clamp stored state too: later refreshes must not restore an obsolete page.
  if (page !== currentPage) setPage(currentPage)
  const displayed = visible.slice(currentPage * 20, (currentPage + 1) * 20)
  return <>
    <div className="request-tools" role="search" aria-label="依頼の絞り込み">
      <label>依頼を検索<input type="search" value={query} maxLength={200} placeholder="件名・相手・依頼ID" onChange={event => { setQuery(event.target.value); setPage(0) }} /></label>
      <label>状態<select value={status} onChange={event => { setStatus(event.target.value); setPage(0) }}>
        <option value="">すべての状態</option>
        {statuses.map(([value, label]) => <option key={value} value={value}>{label}</option>)}
      </select></label>
      <label>並び順<select value={order} onChange={event => { setOrder(event.target.value as RequestOrder); setPage(0) }}>
        <option value="newest">作成が新しい順</option>
        <option value="deadline">期限が近い順</option>
        <option value="priority">優先度が高い順</option>
      </select></label>
      <button type="button" className="secondary-button" disabled={!query && !status} onClick={() => { setQuery(''); setStatus(''); setPage(0) }}>絞り込みを解除</button>
      <span className="request-count" aria-live="polite">{requests.length}件中 {visible.length}件{pages > 1 ? `に一致・${currentPage * 20 + 1}〜${currentPage * 20 + displayed.length}件を表示` : 'を表示'}</span>
    </div>
    {requests.length > 0 && visible.length === 0 ? <p className="people-status">条件に一致する依頼はありません。検索語や状態を変更してください。</p> : null}
    {children(displayed)}
    {pages > 1 ? <nav className="request-pagination" aria-label="依頼のページ送り">
      <button type="button" className="secondary-button" disabled={currentPage === 0} onClick={() => setPage(currentPage - 1)}>前のページ</button>
      <span aria-live="polite">{currentPage + 1} / {pages} ページ</span>
      <button type="button" className="secondary-button" disabled={currentPage === pages - 1} onClick={() => setPage(currentPage + 1)}>次のページ</button>
    </nav> : null}
  </>
}
