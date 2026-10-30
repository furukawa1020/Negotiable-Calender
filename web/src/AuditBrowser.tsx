import { useState, type ReactNode } from 'react'
import type { AuditEvent } from './auditResponse'
import { LoadedPagination } from './LoadedPagination'
import { useLoadedPage } from './useLoadedPage'
import { auditLocalDate } from './auditLocalDate'

const normalized = (value: string) => value.normalize('NFKC').toLocaleLowerCase('ja-JP')
export function AuditBrowser({ events, children }: { events: AuditEvent[]; children: (rows: AuditEvent[]) => ReactNode }) {
  const [query, setQuery] = useState('')
  const [action, setAction] = useState('')
  const [actor, setActor] = useState('')
  const [resourceType, setResourceType] = useState('')
  const [resourceID, setResourceID] = useState('')
  const resourceTypes = [...new Set([...events.map(row => row.resourceType), ...(resourceType ? [resourceType] : [])])].sort()
  const actors = [...new Set([...events.map(row => row.actorUserId), ...(actor ? [actor] : [])])].sort()
  const [from, setFrom] = useState('')
  const [to, setTo] = useState('')
  const invalidRange = Boolean(from && to && from > to)
  const terms = normalized(query).trim().split(/\s+/).filter(Boolean)
  const actions = [...new Set([...events.map(row => row.action), ...(action ? [action] : [])])].sort()
  const visible = events.filter(row => (!action || row.action === action)
    && (!actor || row.actorUserId === actor)
    && (!resourceType || row.resourceType === resourceType)
    && (!resourceID || row.resourceId === resourceID)
    && !invalidRange && (!from || auditLocalDate(row.createdAt) >= from)
    && (!to || (auditLocalDate(row.createdAt) !== '' && auditLocalDate(row.createdAt) <= to))
    && terms.every(term => normalized(`${row.actorUserId} ${row.resourceType} ${row.resourceId} ${row.action}`).includes(term)))
    .sort((a, b) => Date.parse(b.createdAt) - Date.parse(a.createdAt) || a.id.localeCompare(b.id))
  const page = useLoadedPage(visible)
  return <>
    <div className="activity-tools" role="search" aria-label="操作履歴の絞り込み">
      <label>操作履歴を検索<input type="search" maxLength={200} value={query} placeholder="実行者・対象ID" onChange={event => { setQuery(event.target.value); page.setPage(0) }} /></label>
      <label>操作種別<select value={action} onChange={event => { setAction(event.target.value); page.setPage(0) }}><option value="">すべての操作</option>
        {actions.map(item => <option value={item} key={item}>{item.replaceAll('_', ' ')}</option>)}
      </select></label>
      <label>履歴の開始日<input type="date" value={from} onChange={event => { setFrom(event.target.value); page.setPage(0) }} /></label>
      <label>実行者ID<select value={actor} onChange={event => { setActor(event.target.value); page.setPage(0) }}><option value="">すべての実行者</option>
        {actors.map(id => <option key={id} value={id}>{id}</option>)}
      </select></label>
      <label>履歴の終了日<input type="date" value={to} onChange={event => { setTo(event.target.value); page.setPage(0) }} /></label>
      <label>対象種別<select value={resourceType} onChange={event => { setResourceType(event.target.value); page.setPage(0) }}><option value="">すべての対象</option>
        {resourceTypes.map(item => <option key={item} value={item}>{item}</option>)}
      </select></label>
      <label>対象ID（完全一致）<input type="text" value={resourceID} maxLength={200} onChange={event => { setResourceID(event.target.value); page.setPage(0) }} /></label>
      <span>端末の時間帯で両端の日付を含めて検索</span>
      <button className="secondary-button" type="button" disabled={!query && !action && !actor && !resourceType && !resourceID && !from && !to} onClick={() => { setQuery(''); setAction(''); setActor(''); setResourceType(''); setResourceID(''); setFrom(''); setTo(''); page.setPage(0) }}>履歴の絞り込みを解除</button>
      <span aria-live="polite">取得済み{events.length}件中 {visible.length}件が一致・{page.rows.length}件を表示・新しい順</span>
    </div>
    {invalidRange ? <p role="alert">終了日は開始日以降にしてください</p> : null}
    {events.length > 0 && visible.length === 0 ? <p className="people-status">条件に一致する操作履歴はありません</p> : null}
    {children(page.rows)}
    <LoadedPagination label="操作履歴" {...page} />
  </>
}
