const privateEvents = [
  { time: '09:00', label: 'Product Review', size: 'short' },
  { time: '10:00', label: 'Customer Meeting', size: 'medium' },
  { time: '11:30', label: 'Focus', size: 'large' },
  { time: '13:00', label: 'Recruiting Interview', size: 'large' },
]

const projections = [
  { time: '09:00 — 10:00', label: '相談可能', tone: 'available' },
  { time: '10:00 — 11:30', label: '緊急のみ', tone: 'urgent' },
  { time: '11:30 — 13:00', label: '割り込み非推奨', tone: 'focus' },
  { time: '13:00 — 15:30', label: '対応困難', tone: 'unavailable' },
  { time: '15:30 —', label: '15分相談可能', tone: 'available' },
]

function ShieldIcon() {
  return (
    <svg viewBox="0 0 24 24" aria-hidden="true">
      <path d="M12 3 5.5 5.6v5.7c0 4.2 2.7 7.9 6.5 9.7 3.8-1.8 6.5-5.5 6.5-9.7V5.6L12 3Z" />
      <path d="m9.3 12 1.8 1.8 3.8-4" />
    </svg>
  )
}

function App() {
  return (
    <div className="app-shell">
      <header className="topbar">
        <a className="brand" href="#top" aria-label="Negotiable Calendar ホーム">
          <span className="brand-mark" aria-hidden="true">N</span>
          <span>Negotiable Calendar</span>
        </a>
        <div className="topbar-actions">
          <span className="protected-badge"><ShieldIcon />予定詳細は保護されています</span>
          <button className="avatar" type="button" aria-label="山田太郎のアカウントメニュー">山</button>
        </div>
      </header>

      <main id="top">
        <section className="hero" aria-labelledby="page-title">
          <div>
            <p className="eyebrow">SUNDAY · 23 AUGUST</p>
            <h1 id="page-title">今日、どう関われるか。</h1>
            <p className="hero-copy">予定の中身はあなたのもの。組織には、調整に必要な余地だけを共有します。</p>
          </div>
          <button className="primary-button" type="button">
            <span aria-hidden="true">＋</span> 依頼を作成
          </button>
        </section>

        <section className="privacy-note" aria-label="プライバシー設定の状態">
          <ShieldIcon />
          <div>
            <strong>Privacy Projection is active</strong>
            <span>イベント名・参加者・場所は組織に共有されません</span>
          </div>
          <button type="button">共有ルールを確認</button>
        </section>

        <section className="calendar-grid" aria-label="今日のプライベート予定と公開状態">
          <article className="calendar-panel private-panel">
            <div className="panel-heading">
              <div>
                <p>MY CALENDAR</p>
                <h2>あなたの予定</h2>
              </div>
              <span className="private-label">PRIVATE</span>
            </div>
            <div className="private-list">
              {privateEvents.map((event) => (
                <div className="private-row" key={`${event.time}-${event.label}`}>
                  <time>{event.time}</time>
                  <div className={`private-event ${event.size}`}>
                    <strong>{event.label}</strong>
                    <span>非公開の予定</span>
                  </div>
                </div>
              ))}
            </div>
          </article>

          <article className="calendar-panel projection-panel">
            <div className="panel-heading">
              <div>
                <p>WHAT OTHERS SEE</p>
                <h2>組織に見える状態</h2>
              </div>
              <span className="projection-label"><span /> PROJECTION</span>
            </div>
            <div className="projection-list">
              {projections.map((projection) => (
                <div className={`projection-row ${projection.tone}`} key={projection.time}>
                  <time>{projection.time}</time>
                  <strong>{projection.label}</strong>
                  <span className="state-dot" aria-hidden="true" />
                </div>
              ))}
            </div>
            <button className="preview-button" type="button">
              メンバー表示をプレビュー <span aria-hidden="true">→</span>
            </button>
          </article>
        </section>
      </main>

      <footer>
        <span>予定を見せずに、予定を共有する。</span>
        <span>All times JST</span>
      </footer>
    </div>
  )
}

export default App
