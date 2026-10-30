import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it } from 'vitest'
import { AuditBrowser } from './AuditBrowser'
import { auditActionLabel } from './auditActionLabel'
import type { AuditEvent } from './auditResponse'
afterEach(cleanup)
it('labels all emitted actions and safely falls back for unknown and prototype keys', () => {
 const actions = ['request_created', 'request_accepted', 'request_changed', 'reschedule_proposed', 'reschedule_accepted', 'reschedule_declined', 'reschedule_withdrawn', 'request_declined', 'request_async', 'request_delegated', 'request_cancelled', 'request_completed', 'invitation_created', 'invitation_accepted', 'workspace_switched']
 for (const action of actions) expect(auditActionLabel(action)).not.toBe(action)
 for (const action of ['future_action', '__proto__', 'constructor']) expect(auditActionLabel(action)).toBe(action)
 expect(auditActionLabel('reschedule_accepted')).toBe('日時変更の承認')
})
it('searches by Japanese action label or original code combined with actor and exact action', () => {
 const values: AuditEvent[] = ['request_created', 'reschedule_accepted'].map(action => ({ id: action, action, actorUserId: 'alice', organizationId: 'org', resourceId: 'r', resourceType: 'request', createdAt: '2030-01-01T00:00:00Z' }))
 render(<AuditBrowser events={values}>{rows => <ul>{rows.map(row => <li key={row.id}>{row.id}</li>)}</ul>}</AuditBrowser>)
 expect(screen.getByRole('option', { name: '日時変更の承認' })).toHaveValue('reschedule_accepted')
 fireEvent.change(screen.getByRole('searchbox'), { target: { value: 'ＡＬＩＣＥ 日時変更' } })
 expect(screen.getByRole('listitem')).toHaveTextContent('reschedule_accepted')
 fireEvent.change(screen.getByRole('searchbox'), { target: { value: 'request_created' } })
 expect(screen.getByRole('listitem')).toHaveTextContent('request_created')
 fireEvent.change(screen.getByLabelText('操作種別'), { target: { value: 'reschedule_accepted' } })
 expect(screen.queryAllByRole('listitem')).toHaveLength(0)
})
