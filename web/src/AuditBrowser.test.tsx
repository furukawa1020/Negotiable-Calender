import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it } from 'vitest'
import { AuditBrowser } from './AuditBrowser'
import type { AuditEvent } from './auditResponse'

const base: AuditEvent = { id: 'b', organizationId: 'org', actorUserId: 'ＡＬＩＣＥ', action: 'request_created', resourceType: 'request', resourceId: 'r-1', createdAt: '2030-01-01T00:00:00Z' }
const events = [base, { ...base, id: 'a', resourceId: 'r-2', action: 'new_action' }, { ...base, id: 'c', actorUserId: 'bob', resourceId: 'r-3', createdAt: '2030-01-02T00:00:00Z' }]
const rows = (items: AuditEvent[]) => <ul>{items.map(item => <li key={item.id}>{item.resourceId}</li>)}</ul>
afterEach(cleanup)
it('sorts a copy, combines actor/resource search and supports new action types', () => {
  render(<AuditBrowser events={events}>{rows}</AuditBrowser>)
  expect(screen.getAllByRole('listitem').map(row => row.textContent)).toEqual(['r-3', 'r-2', 'r-1'])
  expect(events.map(row => row.id)).toEqual(['b', 'a', 'c'])
  fireEvent.change(screen.getByRole('searchbox'), { target: { value: 'alice　r-2' } })
  expect(screen.getAllByRole('listitem')).toHaveLength(1)
  fireEvent.change(screen.getByRole('combobox'), { target: { value: 'request_created' } })
  expect(screen.getByText('条件に一致する操作履歴はありません')).toBeInTheDocument()
  fireEvent.change(screen.getByRole('combobox'), { target: { value: 'new_action' } })
  expect(screen.getByRole('listitem')).toHaveTextContent('r-2')
  fireEvent.click(screen.getByRole('button', { name: '履歴の絞り込みを解除' }))
  expect(screen.getAllByRole('listitem')).toHaveLength(3)
})
it('retains a selected action through refresh and resets at workspace boundaries', () => {
  const view = render(<AuditBrowser key="a" events={events}>{rows}</AuditBrowser>)
  fireEvent.change(screen.getByRole('combobox'), { target: { value: 'new_action' } })
  view.rerender(<AuditBrowser key="a" events={[base]}>{rows}</AuditBrowser>)
  expect(screen.getByRole('combobox')).toHaveValue('new_action')
  expect(screen.queryAllByRole('listitem')).toHaveLength(0)
  view.rerender(<AuditBrowser key="b" events={[base]}>{rows}</AuditBrowser>)
  expect(screen.getByRole('combobox')).toHaveValue('')
  expect(screen.getAllByRole('listitem')).toHaveLength(1)
})
