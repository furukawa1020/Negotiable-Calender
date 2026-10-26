export function LoadedPagination({ label, page, pages, setPage }: { label: string; page: number; pages: number; setPage: (value: number) => void }) {
  return pages > 1 ? <nav className="request-pagination" aria-label={`${label}のページ送り`}>
    <button type="button" className="secondary-button" disabled={page === 0} onClick={() => setPage(page - 1)}>前のページ</button>
    <span aria-live="polite">{page + 1} / {pages} ページ</span>
    <button type="button" className="secondary-button" disabled={page === pages - 1} onClick={() => setPage(page + 1)}>次のページ</button>
  </nav> : null
}
