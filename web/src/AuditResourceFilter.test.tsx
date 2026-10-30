import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it } from 'vitest'
import { AuditBrowser } from './AuditBrowser'
import type { AuditEvent } from './auditResponse'
afterEach(cleanup)
const base: AuditEvent = { id: 'a', organizationId: 'org', actorUserId: 'alice', action: 'request_created', resourceType: 'request', resourceId: 'r', createdAt: '2030-01-01T00:00:00Z' }
const values = [base, { ...base, id: 'b', resourceId: 'r-other' }, { ...base, id: 'c', resourceType: 'invitation' }]
const rows = (items: AuditEvent[]) => <ul>{items.map(row => <li key={row.id}>{row.id}</li>)}</ul>
it('distinguishes same IDs in other resource types and prefix IDs, retains vanished types and resets by scope', () => {
 const view = render(<AuditBrowser key="a" events={values}>{rows}</AuditBrowser>)
 fireEvent.change(screen.getByLabelText('対象ID（完全一致）'), { target: { value: 'r' } })
 expect(screen.getAllByRole('listitem')).toHaveLength(2)
 fireEvent.change(screen.getByLabelText('対象種別'), { target: { value: 'request' } })
 expect(screen.getByRole('listitem')).toHaveTextContent('a')
 fireEvent.change(screen.getByLabelText('実行者ID'), { target: { value: 'alice' } })
 fireEvent.change(screen.getByRole('searchbox'), { target: { value: 'missing' } })
 expect(screen.queryAllByRole('listitem')).toHaveLength(0)
 fireEvent.change(screen.getByRole('searchbox'), { target: { value: '' } })
 view.rerender(<AuditBrowser key="a" events={[values[2]]}>{rows}</AuditBrowser>)
 expect(screen.getByLabelText('対象種別')).toHaveValue('request')
 expect(screen.queryAllByRole('listitem')).toHaveLength(0)
 fireEvent.click(screen.getByRole('button', { name: '履歴の絞り込みを解除' }))
 expect(screen.getByLabelText('対象ID（完全一致）')).toHaveValue('')
 expect(screen.getByRole('listitem')).toHaveTextContent('c')
 fireEvent.change(screen.getByLabelText('対象ID（完全一致）'), { target: { value: 'r' } })
 view.rerender(<AuditBrowser key="b" events={values}>{rows}</AuditBrowser>)
 expect(screen.getByLabelText('対象ID（完全一致）')).toHaveValue('')
})
