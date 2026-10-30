import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it } from 'vitest'
import { AuditBrowser } from './AuditBrowser'
import type { AuditEvent } from './auditResponse'
afterEach(cleanup)
const base: AuditEvent = { id: 'a', organizationId: 'org', actorUserId: 'alice-other', action: 'request_created', resourceType: 'request', resourceId: 'r', createdAt: '2030-01-01T00:00:00Z' }
const values = [...Array.from({ length: 22 }, (_, i) => ({ ...base, id: `a${i}` })), { ...base, id: 'exact', actorUserId: 'alice' }]
const rows = (items: AuditEvent[]) => <ul>{items.map(row => <li key={row.id}>{row.id}</li>)}</ul>
it('selects an exact actor, resets page, composes search and keeps a disappearing selection until reset', () => {
 const view = render(<AuditBrowser key="a" events={values}>{rows}</AuditBrowser>)
 fireEvent.click(screen.getByRole('button', { name: '次のページ' }))
 fireEvent.change(screen.getByLabelText('実行者ID'), { target: { value: 'alice' } })
 expect(screen.getByRole('listitem')).toHaveTextContent('exact')
 fireEvent.change(screen.getByRole('searchbox'), { target: { value: 'missing' } })
 expect(screen.queryAllByRole('listitem')).toHaveLength(0)
 fireEvent.change(screen.getByRole('searchbox'), { target: { value: '' } })
 view.rerender(<AuditBrowser key="a" events={[base]}>{rows}</AuditBrowser>)
 expect(screen.getByLabelText('実行者ID')).toHaveValue('alice')
 expect(screen.queryAllByRole('listitem')).toHaveLength(0)
 fireEvent.click(screen.getByRole('button', { name: '履歴の絞り込みを解除' }))
 expect(screen.getByRole('listitem')).toHaveTextContent('a')
 fireEvent.change(screen.getByLabelText('実行者ID'), { target: { value: 'alice-other' } })
 view.rerender(<AuditBrowser key="b" events={values}>{rows}</AuditBrowser>)
 expect(screen.getByLabelText('実行者ID')).toHaveValue('')
})
