import { type FormEvent, useState } from 'react'

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
  const [activeDialog, setActiveDialog] = useState('')
  const [memberPreview, setMemberPreview] = useState(false)
  const [accountOpen, setAccountOpen] = useState(false)
  const [notice, setNotice] = useState('')

  const submitRequest = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    setActiveDialog('')
    setNotice('レビュー依頼を送信しました。候補時間を生成しています。')
  }

  return (
    <div className="app-shell">
      <header className="topbar">
        <a className="brand" href="#top" aria-label="Negotiable Calendar ホーム">
          <span className="brand-mark" aria-hidden="true">N</span>
          <span>Negotiable Calendar</span>
        </a>
        <div className="topbar-actions">
          <span className="protected-badge"><ShieldIcon />予定詳細は保護されています</span>
          <div className="account-wrap">
            <button className="avatar" type="button" aria-label="山田太郎のアカウントメニュー" aria-expanded={accountOpen} onClick={() => setAccountOpen(!accountOpen)}>山</button>
            {accountOpen ? (
              <div className="account-menu">
                <strong>山田 太郎</strong>
                <span>Manager · Asia/Tokyo</span>
                <button type="button" onClick={() => setNotice('設定画面は次の実装ステップです。')}>アカウント設定</button>
              </div>
            ) : null}
          </div>
        </div>
      </header>

      <main id="top">
        <section className="hero" aria-labelledby="page-title">
          <div>
            <p className="eyebrow">SUNDAY · 23 AUGUST</p>
            <h1 id="page-title">今日、どう関われるか。</h1>
            <p className="hero-copy">予定の中身はあなたのもの。組織には、調整に必要な余地だけを共有します。</p>
          </div>
          <button className="primary-button" type="button" onClick={() => setActiveDialog('request')}>
            <span aria-hidden="true">＋</span> 依頼を作成
          </button>
        </section>

        <section className="privacy-note" aria-label="プライバシー設定の状態">
          <ShieldIcon />
          <div>
            <strong>Privacy Projection is active</strong>
            <span>イベント名・参加者・場所は組織に共有されません</span>
          </div>
          <button type="button" onClick={() => setActiveDialog('rules')}>共有ルールを確認</button>
        </section>

        <section className={memberPreview ? 'calendar-grid member-preview' : 'calendar-grid'} aria-label="今日のプライベート予定と公開状態">
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
            <button className="preview-button" type="button" onClick={() => setMemberPreview(!memberPreview)}>
              {memberPreview ? '自分の表示に戻る' : 'メンバー表示をプレビュー'} <span aria-hidden="true">→</span>
            </button>
          </article>
        </section>
      </main>

      {activeDialog === 'request' ? (
        <div className="modal-backdrop" role="presentation">
          <section className="modal" role="dialog" aria-modal="true" aria-labelledby="request-title">
            <div className="modal-heading">
              <div>
                <p className="eyebrow">COORDINATION REQUEST</p>
                <h2 id="request-title">依頼を作成</h2>
              </div>
              <button className="close-button" type="button" aria-label="閉じる" onClick={() => setActiveDialog('')}>×</button>
            </div>
            <form className="request-form" onSubmit={submitRequest}>
              <label>
                依頼内容
                <input name="title" defaultValue="新API設計レビュー" required />
              </label>
              <div className="form-row">
                <label>
                  必要時間
                  <select name="duration" defaultValue="15">
                    <option value="5">5分</option>
                    <option value="15">15分</option>
                    <option value="30">30分</option>
                  </select>
                </label>
                <label>
                  期限
                  <input name="deadline" type="time" defaultValue="17:00" required />
                </label>
              </div>
              <label>
                同期性
                <select name="sync" defaultValue="either">
                  <option value="either">できれば会話・非同期でも可</option>
                  <option value="meeting">直接相談したい</option>
                  <option value="async">非同期回答でよい</option>
                </select>
              </label>
              <label>
                重要度
                <select name="priority" defaultValue="normal">
                  <option value="normal">通常</option>
                  <option value="high">高</option>
                  <option value="urgent">緊急</option>
                </select>
              </label>
              <div className="modal-actions">
                <button className="secondary-button" type="button" onClick={() => setActiveDialog('')}>キャンセル</button>
                <button className="primary-button" type="submit">候補を生成して送信</button>
              </div>
            </form>
          </section>
        </div>
      ) : null}

      {activeDialog === 'rules' ? (
        <div className="modal-backdrop" role="presentation">
          <section className="modal rules-modal" role="dialog" aria-modal="true" aria-labelledby="rules-title">
            <div className="modal-heading">
              <div>
                <p className="eyebrow">SHARING POLICY</p>
                <h2 id="rules-title">共有ルール</h2>
              </div>
              <button className="close-button" type="button" aria-label="閉じる" onClick={() => setActiveDialog('')}>×</button>
            </div>
            <p className="modal-copy">予定の内容ではなく、関わりやすさだけに変換して共有します。</p>
            <div className="rule-list">
              <div><span>勤務時間</span><strong>平日 09:00 — 18:00</strong></div>
              <div><span>予定中</span><strong>緊急のみ</strong></div>
              <div><span>集中時間</span><strong>割り込み非推奨</strong></div>
              <div><span>空き時間</span><strong>相談可能</strong></div>
              <div><span>勤務時間外</span><strong>対応不可</strong></div>
            </div>
            <button className="primary-button full-button" type="button" onClick={() => { setActiveDialog(''); setNotice('共有ルールを保存しました。') }}>このルールを保存</button>
          </section>
        </div>
      ) : null}

      {notice ? (
        <div className="toast" role="status">
          <span>✓</span><strong>{notice}</strong>
          <button type="button" aria-label="通知を閉じる" onClick={() => setNotice('')}>×</button>
        </div>
      ) : null}

      <footer>
        <span>予定を見せずに、予定を共有する。</span>
        <span>All times JST</span>
      </footer>
    </div>
  )
}

export default App
