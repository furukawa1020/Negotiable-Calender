import { useState } from 'react'

// These controls page only the loaded snapshot, never imply a server-wide query.
export function useLoadedPage<T>(rows: T[]) {
  const [page, setPage] = useState(0)
  const pages = Math.max(1, Math.ceil(rows.length / 20))
  const current = Math.min(page, pages - 1)
  if (current !== page) setPage(current)
  return { rows: rows.slice(current * 20, (current + 1) * 20), page: current, pages, setPage }
}

export function LoadedPagination({ label, page, pages, setPage }: { label: string; page: number; pages: number; setPage: (value: number) => void }) {
  return pages > 1 ? <nav className="request-pagination" aria-label={`${label}のページ送り`}>
    <button type="button" className="secondary-button" disabled={page === 0} onClick={() => setPage(page - 1)}>前のページ</button>
    <span aria-live="polite">{page + 1} / {pages} ページ</span>
    <button type="button" className="secondary-button" disabled={page === pages - 1} onClick={() => setPage(page + 1)}>次のページ</button>
  </nav> : null
}
