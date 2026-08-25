import { type FormEvent, useMemo, useState } from 'react'

const privateEvents = [
  { time: '09:00', label: 'Product Review', size: 'short' },
  { time: '10:00', label: 'Customer Meeting', size: 'medium' },
  { time: '11:30', label: 'Focus', size: 'large' },
  { time: '13:00', label: 'Recruiting Interview', size: 'large' },
]

const initialProjections = [
  { time: '09:00 — 10:00', label: '相談可能', tone: 'available' },
  { time: '10:00 — 11:30', label: '緊急のみ', tone: 'urgent' },
  { time: '11:30 — 13:00', label: '割り込み非推奨', tone: 'focus' },
  { time: '13:00 — 15:30', label: '対応困難', tone: 'unavailable' },
  { time: '15:30 —', label: '15分相談可能', tone: 'available' },
]

const apiURL = import.meta.env.VITE_API_URL || 'http://localhost:8080'

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
  const [dayOffset, setDayOffset] = useState(0)
  const [calendarLayer, setCalendarLayer] = useState<'both' | 'private' | 'projection'>('both')
  const [projections, setProjections] = useState(initialProjections)
  const [overrideSaving, setOverrideSaving] = useState(false)
  const visibleDate = useMemo(() => {
    const value = new Date()
    value.setDate(value.getDate() + dayOffset)
    return value
  }, [dayOffset])
  const dateLabel = new Intl.DateTimeFormat('ja-JP', {
    month: 'long', day: 'numeric', weekday: 'short',
  }).format(visibleDate)

  const submitRequest = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    setActiveDialog('')
    setNotice('レビュー依頼を送信しました。候補時間を生成しています。')
  }

  const submitOverride = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    const form = new FormData(event.currentTarget)
    const startTime = String(form.get('startTime'))
    const endTime = String(form.get('endTime'))
    const availability = String(form.get('availability'))
    const [startHour, startMinute] = startTime.split(':').map(Number)
    const [endHour, endMinute] = endTime.split(':').map(Number)
    const startAt = new Date(visibleDate)
    startAt.setHours(startHour, startMinute, 0, 0)
    const endAt = new Date(visibleDate)
    endAt.setHours(endHour, endMinute, 0, 0)
    setOverrideSaving(true)
    try {
      const response = await fetch(`${apiURL}/api/v1/users/demo-manager/manual-overrides`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          startAt: startAt.toISOString(),
          endAt: endAt.toISOString(),
          expiresAt: new Date(endAt.getTime() + 24 * 60 * 60 * 1000).toISOString(),
          state: {
            availability,
            interruptibility: availability === 'available' ? 'open' : 'urgent_only',
            requestability: availability === 'available' ? 'open' : 'later',
            reschedulability: availability === 'available' ? 'high' : 'low',
          },
        }),
      })
      if (!response.ok) {
        throw new Error('save failed')
      }
      const label = availability === 'available' ? '相談可能（上書き）' : '緊急のみ（上書き）'
      setProjections((current) => [
        ...current,
        { time: `${startTime} — ${endTime}`, label, tone: availability === 'available' ? 'available' : 'urgent' },
      ])
      setActiveDialog('')
      setNotice('公開状態を上書きしました。')
    } catch {
      setNotice('保存できませんでした。APIの接続を確認してください。')
    } finally {
      setOverrideSaving(false)
    }
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
            <p className="eyebrow">{dateLabel}</p>
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

        <section className="calendar-toolbar" aria-label="カレンダー操作">
          <div className="date-controls">
            <button type="button" aria-label="前の日" onClick={() => setDayOffset((value) => value - 1)}>←</button>
            <button type="button" onClick={() => setDayOffset(0)}>今日</button>
            <button type="button" aria-label="次の日" onClick={() => setDayOffset((value) => value + 1)}>→</button>
          </div>
          <div className="layer-controls" aria-label="表示レイヤー">
            <button className={calendarLayer === 'both' ? 'active' : ''} type="button" onClick={() => setCalendarLayer('both')}>両方</button>
            <button className={calendarLayer === 'private' ? 'active' : ''} type="button" onClick={() => setCalendarLayer('private')}>Private</button>
            <button className={calendarLayer === 'projection' ? 'active' : ''} type="button" onClick={() => setCalendarLayer('projection')}>Projection</button>
          </div>
          <button className="override-button" type="button" onClick={() => setActiveDialog('override')}>状態を上書き</button>
        </section>

        <section className={memberPreview ? 'calendar-grid member-preview' : `calendar-grid layer-${calendarLayer}`} aria-label="今日のプライベート予定と公開状態">
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

      {activeDialog === 'override' ? (
        <div className="modal-backdrop" role="presentation">
          <section className="modal" role="dialog" aria-modal="true" aria-labelledby="override-title">
            <div className="modal-heading">
              <div>
                <p className="eyebrow">MANUAL OVERRIDE</p>
                <h2 id="override-title">公開状態を上書き</h2>
              </div>
              <button className="close-button" type="button" aria-label="閉じる" onClick={() => setActiveDialog('')}>×</button>
            </div>
            <p className="modal-copy">予定の内容は変更せず、組織に見える関わりやすさだけを一時変更します。</p>
            <form className="request-form" onSubmit={submitOverride}>
              <div className="form-row">
                <label>開始<input name="startTime" type="time" defaultValue="15:30" required /></label>
                <label>終了<input name="endTime" type="time" defaultValue="16:00" required /></label>
              </div>
              <label>
                公開状態
                <select name="availability" defaultValue="available">
                  <option value="available">相談可能</option>
                  <option value="limited">緊急のみ</option>
                </select>
              </label>
              <div className="modal-actions">
                <button className="secondary-button" type="button" onClick={() => setActiveDialog('')}>キャンセル</button>
                <button className="primary-button" type="submit" disabled={overrideSaving}>{overrideSaving ? '保存中…' : '上書きを保存'}</button>
              </div>
            </form>
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
