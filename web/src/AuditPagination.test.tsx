import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it } from 'vitest'
import { AuditBrowser } from './AuditBrowser'
import type { AuditEvent } from './auditResponse'
afterEach(cleanup)
const values: AuditEvent[] = Array.from({ length: 41 }, (_, i) => ({ id: String(i).padStart(3, '0'), organizationId: 'org', actorUserId: 'a', action: 'request_created', resourceId: `r${i}`, resourceType: 'request', createdAt: '2030-01-01T00:00:00Z' }))
const rows = (items: AuditEvent[]) => <ul>{items.map(row => <li key={row.id}>{row.resourceId}</li>)}</ul>
it('pages history, resets on action/search changes and clamps refreshed snapshots', () => {
 const view = render(<AuditBrowser events={values}>{rows}</AuditBrowser>)
 expect(screen.getAllByRole('listitem')).toHaveLength(20)
 fireEvent.click(screen.getByRole('button', { name: '次のページ' }))
 expect(screen.getAllByRole('listitem')[0]).toHaveTextContent('r20')
 fireEvent.click(screen.getByRole('button', { name: '次のページ' }))
 expect(screen.getAllByRole('listitem')).toHaveLength(1)
 view.rerender(<AuditBrowser events={values.slice(0,21)}>{rows}</AuditBrowser>)
 expect(screen.getByText('2 / 2 ページ')).toBeInTheDocument()
 fireEvent.change(screen.getByLabelText('操作種別'), { target: { value: 'request_created' } })
 expect(screen.getByText('1 / 2 ページ')).toBeInTheDocument()
 fireEvent.click(screen.getByRole('button', { name: '次のページ' }))
 fireEvent.change(screen.getByRole('searchbox'), { target: { value: 'r0' } })
 expect(screen.getAllByRole('listitem')).toHaveLength(1)
 fireEvent.click(screen.getByRole('button', { name: '履歴の絞り込みを解除' }))
 expect(screen.getByText('1 / 2 ページ')).toBeInTheDocument()
 fireEvent.click(screen.getByRole('button', { name: '次のページ' }))
 view.rerender(<AuditBrowser events={[]}>{rows}</AuditBrowser>)
 view.rerender(<AuditBrowser events={values}>{rows}</AuditBrowser>)
 expect(screen.getByText('1 / 3 ページ')).toBeInTheDocument()
})
