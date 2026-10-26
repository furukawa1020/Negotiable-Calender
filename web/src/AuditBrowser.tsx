import { useState, type ReactNode } from 'react'
import type { AuditEvent } from './auditResponse'
import { LoadedPagination, useLoadedPage } from './LoadedPagination'

const normalized = (value: string) => value.normalize('NFKC').toLocaleLowerCase('ja-JP')
export function AuditBrowser({ events, children }: { events: AuditEvent[]; children: (rows: AuditEvent[]) => ReactNode }) {
  const [query, setQuery] = useState('')
  const [action, setAction] = useState('')
  const terms = normalized(query).trim().split(/\s+/).filter(Boolean)
  const actions = [...new Set([...events.map(row => row.action), ...(action ? [action] : [])])].sort()
  const visible = events.filter(row => (!action || row.action === action)
    && terms.every(term => normalized(`${row.actorUserId} ${row.resourceType} ${row.resourceId} ${row.action}`).includes(term)))
    .sort((a, b) => Date.parse(b.createdAt) - Date.parse(a.createdAt) || a.id.localeCompare(b.id))
  const page = useLoadedPage(visible)
  return <>
    <div className="activity-tools" role="search" aria-label="操作履歴の絞り込み">
      <label>操作履歴を検索<input type="search" maxLength={200} value={query} placeholder="実行者・対象ID" onChange={event => { setQuery(event.target.value); page.setPage(0) }} /></label>
      <label>操作種別<select value={action} onChange={event => { setAction(event.target.value); page.setPage(0) }}><option value="">すべての操作</option>
        {actions.map(item => <option value={item} key={item}>{item.replaceAll('_', ' ')}</option>)}
      </select></label>
      <button className="secondary-button" type="button" disabled={!query && !action} onClick={() => { setQuery(''); setAction(''); page.setPage(0) }}>履歴の絞り込みを解除</button>
      <span aria-live="polite">取得済み{events.length}件中 {visible.length}件が一致・{page.rows.length}件を表示・新しい順</span>
    </div>
    {events.length > 0 && visible.length === 0 ? <p className="people-status">条件に一致する操作履歴はありません</p> : null}
    {children(page.rows)}
    <LoadedPagination label="操作履歴" {...page} />
  </>
}
