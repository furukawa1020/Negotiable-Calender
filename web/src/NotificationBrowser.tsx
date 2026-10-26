import { useState, type ReactNode } from 'react'
import type { AppNotification } from './notificationResponse'
import { LoadedPagination } from './LoadedPagination'
import { useLoadedPage } from './useLoadedPage'

const normalized = (value: string) => value.normalize('NFKC').toLocaleLowerCase('ja-JP')
const typeLabels: Record<string, string> = { request_received: '依頼の受信', request_accepted: '日時の確定', request_changed: '依頼の変更', request_delegated: '担当の変更', request_declined: '依頼の辞退' }
export function NotificationBrowser({ notifications, children }: { notifications: AppNotification[]; children: (rows: AppNotification[]) => ReactNode }) {
  const [query, setQuery] = useState('')
  const [unreadOnly, setUnreadOnly] = useState(false)
  const [type, setType] = useState('')
  const types = [...new Set([...notifications.map(row => row.type), ...(type ? [type] : [])])].sort()
  const terms = normalized(query).trim().split(/\s+/).filter(Boolean)
  const visible = notifications.filter(row => (!unreadOnly || !row.readAt)
    && (!type || row.type === type)
    && terms.every(term => normalized(`${row.message} ${row.requestId} ${row.type}`).includes(term)))
    .sort((a, b) => Date.parse(b.createdAt) - Date.parse(a.createdAt) || a.id.localeCompare(b.id))
  const page = useLoadedPage(visible)
  return <>
    <div className="activity-tools" role="search" aria-label="通知の絞り込み">
      <label>通知を検索<input type="search" maxLength={200} value={query} placeholder="通知文・依頼ID" onChange={event => { setQuery(event.target.value); page.setPage(0) }} /></label>
      <label className="activity-checkbox"><input type="checkbox" checked={unreadOnly} onChange={event => { setUnreadOnly(event.target.checked); page.setPage(0) }} />未読のみ</label>
      <label>通知種別<select value={type} onChange={event => { setType(event.target.value); page.setPage(0) }}><option value="">すべての通知</option>
        {types.map(item => <option key={item} value={item}>{Object.hasOwn(typeLabels, item) ? typeLabels[item] : item}</option>)}
      </select></label>
      <button className="secondary-button" type="button" disabled={!query && !unreadOnly && !type} onClick={() => { setQuery(''); setUnreadOnly(false); setType(''); page.setPage(0) }}>通知の絞り込みを解除</button>
      <span aria-live="polite">取得済み{notifications.length}件中 {visible.length}件が一致・{page.rows.length}件を表示・新しい順</span>
    </div>
    {notifications.length > 0 && visible.length === 0 ? <p>条件に一致する通知はありません</p> : null}
    {children(page.rows)}
    <LoadedPagination label="通知" {...page} />
  </>
}
