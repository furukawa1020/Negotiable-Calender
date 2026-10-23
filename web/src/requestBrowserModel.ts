import type { CoordinationRequest } from './bookingResponse'
import type { RequestPeople } from './requestPeople'
const normalize = (value: string) => value.normalize('NFKC').toLocaleLowerCase('ja-JP').trim()
export type RequestOrder = 'newest' | 'deadline' | 'priority'
const priorities: Record<string, number> = { urgent: 3, high: 2, normal: 1, low: 0 }
export function orderRequests(requests: CoordinationRequest[], order: RequestOrder): CoordinationRequest[] {
  return [...requests].sort((a, b) => {
    const deadline = Date.parse(a.deadlineAt) - Date.parse(b.deadlineAt)
    const primary = order === 'deadline' ? deadline : order === 'priority'
      ? (priorities[b.priority] ?? -1) - (priorities[a.priority] ?? -1)
      : Date.parse(b.createdAt) - Date.parse(a.createdAt)
    return primary || deadline || (a.id < b.id ? -1 : a.id > b.id ? 1 : 0)
  })
}

export function filterRequests(requests: CoordinationRequest[], query: string, status: string, type = '', priority = '', names: RequestPeople = {}): CoordinationRequest[] {
  const needles = normalize(query).split(/\s+/).filter(Boolean)
  return requests.filter(row => (status === '' || row.status === status)
    && (type === '' || row.type === type) && (priority === '' || row.priority === priority)
    && needles.every(needle => [row.title, row.requesterUserId, row.targetUserId, row.id,
      Object.hasOwn(names, row.requesterUserId) ? names[row.requesterUserId] : '',
      Object.hasOwn(names, row.targetUserId) ? names[row.targetUserId] : '',
    ].some(value => normalize(value).includes(needle))))
}
