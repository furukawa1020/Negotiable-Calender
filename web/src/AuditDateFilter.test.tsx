import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it } from 'vitest'
import { AuditBrowser } from './AuditBrowser'
import { auditLocalDate } from './auditLocalDate'
import type { AuditEvent } from './auditResponse'
afterEach(cleanup)
const base: AuditEvent = { id: 'a', organizationId: 'org', actorUserId: 'alice', action: 'request_created', resourceId: 'r', resourceType: 'request', createdAt: new Date(2030, 0, 2, 0, 0, 0).toISOString() }
const values = [base, { ...base, id: 'b', createdAt: new Date(2030, 0, 2, 23, 59, 59).toISOString() }, { ...base, id: 'c', createdAt: new Date(2030, 0, 3, 0, 0, 0).toISOString() }]
const rows = (items: AuditEvent[]) => <ul>{items.map(row => <li key={row.id}>{row.id}</li>)}</ul>
it('uses inclusive local dates, supports open endpoints and rejects inverted ranges', () => {
 expect(auditLocalDate(base.createdAt)).toBe('2030-01-02')
 expect(auditLocalDate('invalid')).toBe('')
 render(<AuditBrowser events={values}>{rows}</AuditBrowser>)
 fireEvent.change(screen.getByLabelText('履歴の終了日'), { target: { value: '2030-01-02' } })
 expect(screen.getAllByRole('listitem')).toHaveLength(2)
 fireEvent.change(screen.getByLabelText('履歴の開始日'), { target: { value: '2030-01-02' } })
 expect(screen.getAllByRole('listitem')).toHaveLength(2)
 fireEvent.change(screen.getByRole('searchbox'), { target: { value: 'missing' } })
 expect(screen.queryAllByRole('listitem')).toHaveLength(0)
 fireEvent.change(screen.getByRole('searchbox'), { target: { value: 'alice' } })
 expect(screen.getAllByRole('listitem')).toHaveLength(2)
 fireEvent.change(screen.getByLabelText('履歴の開始日'), { target: { value: '2030-01-03' } })
 expect(screen.getByRole('alert')).toHaveTextContent('終了日は開始日以降')
 expect(screen.queryAllByRole('listitem')).toHaveLength(0)
 fireEvent.change(screen.getByLabelText('履歴の終了日'), { target: { value: '' } })
 expect(screen.getByRole('listitem')).toHaveTextContent('c')
 fireEvent.click(screen.getByRole('button', { name: '履歴の絞り込みを解除' }))
 expect(screen.getByLabelText('履歴の開始日')).toHaveValue('')
 expect(screen.getByLabelText('履歴の終了日')).toHaveValue('')
 expect(screen.getAllByRole('listitem')).toHaveLength(3)
})
