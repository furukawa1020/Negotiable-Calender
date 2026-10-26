import { useState } from 'react'

// These controls page only the loaded snapshot, never imply a server-wide query.
export function useLoadedPage<T>(rows: T[]) {
  const [page, setPage] = useState(0)
  const pages = Math.max(1, Math.ceil(rows.length / 20))
  const current = Math.min(page, pages - 1)
  if (current !== page) setPage(current)
  return { rows: rows.slice(current * 20, (current + 1) * 20), page: current, pages, setPage }
}
