import { useEffect, useRef, useState, type ReactNode } from 'react'
import type { RequestPeople } from './requestPeople'
import type { CoordinationRequest } from './bookingResponse'
import { filterRequests, orderRequests, type RequestOrder } from './requestBrowserModel'

const statuses = [
  ['pending', '受付待ち'], ['suggested', '候補提示済み'], ['accepted', '日時確定'],
  ['delegated', '担当変更済み'], ['async', '非同期回答済み'], ['declined', '辞退'],
  ['cancelled', 'キャンセル'], ['expired', '期限切れ'], ['completed', '完了'],
] as const
const types = [['quick_question', '短い質問'], ['meeting', '会議'], ['review', 'レビュー'], ['approval', '承認'], ['decision', '意思決定'], ['async_response', '非同期の回答'], ['urgent_contact', '緊急連絡']] as const
const priorities = [['urgent', '緊急'], ['high', '高'], ['normal', '通常'], ['low', '低']] as const
export function RequestBrowser({ requests, children, loadPeople, initialRequestID = '' }: { requests: CoordinationRequest[]; children: (rows: CoordinationRequest[]) => ReactNode; loadPeople?: () => Promise<RequestPeople | undefined>; initialRequestID?: string }) {
  const [query, setQuery] = useState(initialRequestID)
  const [status, setStatus] = useState('')
  const [type, setType] = useState('')
  const [priority, setPriority] = useState('')
  const [order, setOrder] = useState<RequestOrder>('newest')
  const [page, setPage] = useState(0)
  const [names, setNames] = useState<RequestPeople>({})
  const [namesLoading, setNamesLoading] = useState(false)
  const [namesMessage, setNamesMessage] = useState('')
  const active = useRef(false)
  const nameOperation = useRef(0)
  useEffect(() => { nameOperation.current++; active.current = true; return () => { active.current = false } }, [])
  const loadNames = async () => {
    if (!loadPeople || namesLoading) return
    const operation = ++nameOperation.current
    const current = () => active.current && operation === nameOperation.current
    setNamesLoading(true)
    setNamesMessage('')
    setNames({})
    try {
      const result = await loadPeople()
      if (!current()) return
      if (!result) throw new Error('directory unavailable')
      setNames(result)
      setPage(0)
      setNamesMessage('組織の氏名を検索に使えます')
    } catch {
      if (current()) setNamesMessage('氏名を取得できませんでした。件名・IDで検索するか再試行してください。')
    } finally {
      if (current()) setNamesLoading(false)
    }
  }
  const visible = orderRequests(filterRequests(requests, query, status, type, priority, names)
    .filter(row => !initialRequestID || query !== initialRequestID || row.id === initialRequestID), order)
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
        <option value="active">未完了のみ（受付待ち・候補提示・日時確定・担当変更）</option>
        {statuses.map(([value, label]) => <option key={value} value={value}>{label}</option>)}
      </select></label>
      <label>並び順<select value={order} onChange={event => { setOrder(event.target.value as RequestOrder); setPage(0) }}>
        <option value="newest">作成が新しい順</option>
        <option value="deadline">期限が近い順</option>
        <option value="priority">優先度が高い順</option>
      </select></label>
      <label>依頼種別<select value={type} onChange={event => { setType(event.target.value); setPage(0) }}>
        <option value="">すべての種別</option>
        {types.map(([value, label]) => <option key={value} value={value}>{label}</option>)}
      </select></label>
      <label>優先度<select value={priority} onChange={event => { setPriority(event.target.value); setPage(0) }}>
        <option value="">すべての優先度</option>
        {priorities.map(([value, label]) => <option key={value} value={value}>{label}</option>)}
      </select></label>
      <button type="button" className="secondary-button" disabled={!query && !status && !type && !priority} onClick={() => { setQuery(''); setStatus(''); setType(''); setPriority(''); setPage(0) }}>絞り込みを解除</button>
      {loadPeople ? <button type="button" className="secondary-button" disabled={namesLoading} onClick={() => { void loadNames() }}>{namesLoading ? '氏名を取得中…' : '検索用の氏名を取得'}</button> : null}
      {namesMessage ? <span className="request-count" aria-live="polite">{namesMessage}</span> : null}
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
