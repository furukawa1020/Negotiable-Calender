import type { CoordinationRequest } from './bookingResponse'
const normalize = (value: string) => value.normalize('NFKC').toLocaleLowerCase('ja-JP').trim()

export function filterRequests(requests: CoordinationRequest[], query: string, status: string): CoordinationRequest[] {
  const needle = normalize(query)
  return requests.filter(row => (status === '' || row.status === status)
    && [row.title, row.requesterUserId, row.targetUserId, row.id].some(value => normalize(value).includes(needle)))
}
