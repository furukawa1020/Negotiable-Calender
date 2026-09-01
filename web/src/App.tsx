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

type ProjectionRow = (typeof initialProjections)[number]

type PublicProjection = {
  segments: Array<{
    startAt: string
    endAt: string
    availability: string
    interruptibility: string
  }>
}

type PersonCard = {
  id: string
  displayName: string
  timezone: string
  role: string
  segments: ProjectionRow[]
}

type CoordinationOption = {
  id: string
  type: 'meeting' | 'async' | 'delegate' | 'decline'
  startAt?: string
  endAt?: string
  responseBy?: string
}

type CoordinationRequest = {
  id: string
  requesterUserId: string
  title: string
  type: string
  durationMinutes: number
  deadlineAt: string
  priority: string
  status: string
  options: CoordinationOption[]
  createdAt: string
}

type AppNotification = {
  id: string
  type: string
  requestId: string
  message: string
  readAt?: string
  createdAt: string
}

type AuditEvent = {
  id: string
  organizationId: string
  actorUserId: string
  action: string
  resourceType: string
  resourceId: string
  createdAt: string
}

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
  const [notificationsOpen, setNotificationsOpen] = useState(false)
  const [notifications, setNotifications] = useState<AppNotification[]>([])
  const [notificationsLoading, setNotificationsLoading] = useState(false)
  const [notice, setNotice] = useState('')
  const [dayOffset, setDayOffset] = useState(0)
  const [calendarLayer, setCalendarLayer] = useState<'both' | 'private' | 'projection'>('both')
  const [projections, setProjections] = useState(initialProjections)
  const [memberProjections, setMemberProjections] = useState<ProjectionRow[]>([])
  const [previewLoading, setPreviewLoading] = useState(false)
  const [currentView, setCurrentView] = useState<'calendar' | 'people' | 'inbox' | 'audit'>('calendar')
  const [people, setPeople] = useState<PersonCard[]>([])
  const [peopleLoading, setPeopleLoading] = useState(false)
  const [peopleError, setPeopleError] = useState('')
  const [requestSaving, setRequestSaving] = useState(false)
  const [inboxRequests, setInboxRequests] = useState<CoordinationRequest[]>([])
  const [inboxLoading, setInboxLoading] = useState(false)
  const [inboxError, setInboxError] = useState('')
  const [respondingRequestID, setRespondingRequestID] = useState('')
  const [auditLogs, setAuditLogs] = useState<AuditEvent[]>([])
  const [auditLoading, setAuditLoading] = useState(false)
  const [auditError, setAuditError] = useState('')
  const [overrideSaving, setOverrideSaving] = useState(false)
  const [exporting, setExporting] = useState(false)
  const visibleDate = useMemo(() => {
    const value = new Date()
    value.setDate(value.getDate() + dayOffset)
    return value
  }, [dayOffset])
  const dateLabel = new Intl.DateTimeFormat('ja-JP', {
    month: 'long', day: 'numeric', weekday: 'short',
  }).format(visibleDate)
  const displayedProjections = memberPreview ? memberProjections : projections

  const submitRequest = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    const form = new FormData(event.currentTarget)
    const deadline = new Date()
    const [hour, minute] = String(form.get('deadline')).split(':').map(Number)
    deadline.setHours(hour, minute, 0, 0)
    if (deadline.getTime() <= Date.now()) {
      deadline.setDate(deadline.getDate() + 1)
    }
    setRequestSaving(true)
    try {
      const response = await fetch(`${apiURL}/api/v1/requests`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'X-Demo-User-ID': 'demo-member',
          'X-Organization-ID': 'demo-org',
        },
        body: JSON.stringify({
          targetUserId: 'demo-manager',
          type: 'review',
          title: String(form.get('title')),
          durationMinutes: Number(form.get('duration')),
          deadlineAt: deadline.toISOString(),
          syncPreference: String(form.get('sync')),
          priority: String(form.get('priority')),
        }),
      })
      if (!response.ok) {
        throw new Error('request failed')
      }
      const created = await response.json() as { options?: unknown[] }
      const optionCount = Array.isArray(created.options) ? created.options.length : 0
      setActiveDialog('')
      setNotice(optionCount > 0
        ? `レビュー依頼を送信し、${optionCount}件の候補を生成しました。`
        : 'レビュー依頼を送信しました。')
    } catch {
      setNotice('依頼を送信できませんでした。入力内容とAPI接続を確認してください。')
    } finally {
      setRequestSaving(false)
    }
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
        headers: { 'Content-Type': 'application/json', 'X-Demo-User-ID': 'demo-manager' },
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

  const toggleMemberPreview = async () => {
    if (memberPreview) {
      setMemberPreview(false)
      return
    }
    const from = new Date(visibleDate)
    from.setHours(0, 0, 0, 0)
    const to = new Date(from)
    to.setDate(to.getDate() + 1)
    setPreviewLoading(true)
    try {
      const query = new URLSearchParams({
        timezone: 'Asia/Tokyo',
        from: from.toISOString(),
        to: to.toISOString(),
      })
      const response = await fetch(`${apiURL}/api/v1/people/demo-manager/projection?${query}`, {
        headers: { 'X-Demo-User-ID': 'demo-manager', 'X-Organization-ID': 'demo-org' },
      })
      if (!response.ok) {
        throw new Error('preview failed')
      }
      const view = await response.json() as PublicProjection
      const formatter = new Intl.DateTimeFormat('ja-JP', { hour: '2-digit', minute: '2-digit', hour12: false })
      setMemberProjections(view.segments.map((segment) => ({
        time: `${formatter.format(new Date(segment.startAt))} — ${formatter.format(new Date(segment.endAt))}`,
        label: segment.availability === 'available'
          ? '相談可能'
          : segment.interruptibility === 'urgent_only' ? '緊急のみ' : '対応困難',
        tone: segment.availability === 'available'
          ? 'available'
          : segment.availability === 'limited' ? 'urgent' : 'unavailable',
      })))
      setMemberPreview(true)
    } catch {
      setNotice('メンバー表示を取得できませんでした。')
    } finally {
      setPreviewLoading(false)
    }
  }

  const mapPublicSegments = (view: PublicProjection): ProjectionRow[] => {
    const formatter = new Intl.DateTimeFormat('ja-JP', { hour: '2-digit', minute: '2-digit', hour12: false })
    return view.segments.map((segment) => ({
      time: `${formatter.format(new Date(segment.startAt))} — ${formatter.format(new Date(segment.endAt))}`,
      label: segment.availability === 'available'
        ? '相談可能'
        : segment.interruptibility === 'urgent_only' ? '緊急のみ' : '対応困難',
      tone: segment.availability === 'available'
        ? 'available'
        : segment.availability === 'limited' ? 'urgent' : 'unavailable',
    }))
  }

  const openPeopleView = async () => {
    setCurrentView('people')
    setPeopleLoading(true)
    setPeopleError('')
    try {
      const response = await fetch(`${apiURL}/api/v1/people?organizationId=demo-org`, {
        headers: { 'X-Demo-User-ID': 'demo-manager', 'X-Organization-ID': 'demo-org' },
      })
      if (!response.ok) {
        throw new Error('people failed')
      }
      const directory = await response.json() as { people: Array<Omit<PersonCard, 'segments'>> }
      const cards = await Promise.all(directory.people.map(async (person) => {
        const projectionResponse = await fetch(
          `${apiURL}/api/v1/people/${person.id}/projection?timezone=${encodeURIComponent(person.timezone)}`,
          { headers: { 'X-Demo-User-ID': 'demo-manager', 'X-Organization-ID': 'demo-org' } },
        )
        if (!projectionResponse.ok) {
          throw new Error('projection failed')
        }
        const view = await projectionResponse.json() as PublicProjection
        return { ...person, segments: mapPublicSegments(view) }
      }))
      setPeople(cards)
    } catch {
      setPeopleError('組織の公開状態を取得できませんでした。')
    } finally {
      setPeopleLoading(false)
    }
  }

  const openInbox = async () => {
    setCurrentView('inbox')
    setInboxLoading(true)
    setInboxError('')
    try {
      const response = await fetch(`${apiURL}/api/v1/requests`, {
        headers: { 'X-Demo-User-ID': 'demo-manager' },
      })
      if (!response.ok) {
        throw new Error('inbox failed')
      }
      const payload = await response.json() as { requests: CoordinationRequest[] }
      setInboxRequests(payload.requests)
    } catch {
      setInboxError('依頼を取得できませんでした。')
    } finally {
      setInboxLoading(false)
    }
  }

  const formatDateTime = (value: string) => new Intl.DateTimeFormat('ja-JP', {
    month: 'numeric', day: 'numeric', hour: '2-digit', minute: '2-digit', hour12: false,
  }).format(new Date(value))

  const openNotifications = async () => {
    const nextOpen = !notificationsOpen
    setNotificationsOpen(nextOpen)
    setAccountOpen(false)
    if (!nextOpen) return
    setNotificationsLoading(true)
    try {
      const response = await fetch(`${apiURL}/api/v1/notifications`, {
        headers: { 'X-Demo-User-ID': 'demo-manager' },
      })
      if (!response.ok) throw new Error('notifications failed')
      const payload = await response.json() as { notifications: AppNotification[] }
      setNotifications(payload.notifications)
    } catch {
      setNotice('通知を取得できませんでした。')
    } finally {
      setNotificationsLoading(false)
    }
  }

  const markNotificationRead = async (id: string) => {
    try {
      const response = await fetch(`${apiURL}/api/v1/notifications/${id}/read`, {
        method: 'POST', headers: { 'X-Demo-User-ID': 'demo-manager' },
      })
      if (!response.ok) throw new Error('read failed')
      setNotifications((current) => current.map((item) => item.id === id
        ? { ...item, readAt: new Date().toISOString() }
        : item))
    } catch {
      setNotice('通知を既読にできませんでした。')
    }
  }

  const openAudit = async () => {
    setCurrentView('audit')
    setAuditLoading(true)
    setAuditError('')
    try {
      const response = await fetch(`${apiURL}/api/v1/audit-logs`, {
        headers: {
          'X-Demo-User-ID': 'demo-manager',
          'X-Organization-ID': 'demo-org',
        },
      })
      if (!response.ok) throw new Error('audit failed')
      const payload = await response.json() as { auditLogs: AuditEvent[] }
      setAuditLogs(payload.auditLogs)
    } catch {
      setAuditError('監査ログを取得できませんでした。')
    } finally {
      setAuditLoading(false)
    }
  }

  const respondToRequest = async (requestID: string, action: 'accept' | 'decline', optionID?: string) => {
    setRespondingRequestID(requestID)
    try {
      const response = await fetch(`${apiURL}/api/v1/requests/${requestID}/${action}`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'X-Demo-User-ID': 'demo-manager',
        },
        body: action === 'accept' ? JSON.stringify({ optionId: optionID }) : undefined,
      })
      if (!response.ok) {
        throw new Error('response failed')
      }
      setInboxRequests((current) => current.map((item) => item.id === requestID
        ? { ...item, status: action === 'accept' ? 'accepted' : 'declined' }
        : item))
      setNotice(action === 'accept' ? '候補を承認しました。' : '依頼を辞退しました。')
    } catch {
      setNotice('依頼を更新できませんでした。最新状態を確認してください。')
    } finally {
      setRespondingRequestID('')
    }
  }

  const suggestTime = async (event: FormEvent<HTMLFormElement>, requestID: string) => {
    event.preventDefault()
    const formElement = event.currentTarget
    const form = new FormData(formElement)
    const startAt = new Date(String(form.get('suggestStart')))
    const endAt = new Date(String(form.get('suggestEnd')))
    setRespondingRequestID(requestID)
    try {
      const response = await fetch(`${apiURL}/api/v1/requests/${requestID}/suggest`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'X-Demo-User-ID': 'demo-manager',
        },
        body: JSON.stringify({ startAt: startAt.toISOString(), endAt: endAt.toISOString() }),
      })
      if (!response.ok) {
        throw new Error('suggest failed')
      }
      const option = await response.json() as CoordinationOption
      setInboxRequests((current) => current.map((item) => item.id === requestID
        ? { ...item, options: [...item.options, option] }
        : item))
      setNotice('別の時間候補を追加しました。')
      formElement.reset()
    } catch {
      setNotice('時間候補を追加できませんでした。未来の日時を確認してください。')
    } finally {
      setRespondingRequestID('')
    }
  }

  const delegateRequest = async (event: FormEvent<HTMLFormElement>, requestID: string) => {
    event.preventDefault()
    const form = new FormData(event.currentTarget)
    const delegateUserId = String(form.get('delegateUserId'))
    setRespondingRequestID(requestID)
    try {
      const response = await fetch(`${apiURL}/api/v1/requests/${requestID}/delegate`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'X-Demo-User-ID': 'demo-manager',
        },
        body: JSON.stringify({ delegateUserId }),
      })
      if (!response.ok) {
        throw new Error('delegate failed')
      }
      setInboxRequests((current) => current.map((item) => item.id === requestID
        ? { ...item, status: 'delegated' }
        : item))
      setNotice(`${delegateUserId} に依頼を委譲しました。`)
    } catch {
      setNotice('委譲できませんでした。同じ組織のユーザーIDを確認してください。')
    } finally {
      setRespondingRequestID('')
    }
  }

  const exportUserData = async () => {
    setExporting(true)
    try {
      const response = await fetch(`${apiURL}/api/v1/users/demo-manager/export`, {
        headers: { 'X-Demo-User-ID': 'demo-manager' },
      })
      if (!response.ok) throw new Error('export failed')
      const blob = await response.blob()
      const url = URL.createObjectURL(blob)
      const link = document.createElement('a')
      link.href = url
      link.download = `negotiable-calendar-demo-manager-${new Date().toISOString().slice(0, 10)}.json`
      document.body.append(link)
      link.click()
      link.remove()
      URL.revokeObjectURL(url)
      setAccountOpen(false)
      setNotice('本人データを安全にエクスポートしました。')
    } catch {
      setNotice('データをエクスポートできませんでした。')
    } finally {
      setExporting(false)
    }
  }

  return (
    <div className="app-shell">
      <header className="topbar">
        <a className="brand" href="#top" aria-label="Negotiable Calendar ホーム">
          <span className="brand-mark" aria-hidden="true">N</span>
          <span>Negotiable Calendar</span>
        </a>
        <nav className="top-nav" aria-label="メインナビゲーション">
          <button className={currentView === 'calendar' ? 'active' : ''} type="button" onClick={() => setCurrentView('calendar')}>マイカレンダー</button>
          <button className={currentView === 'people' ? 'active' : ''} type="button" onClick={openPeopleView}>組織</button>
          <button className={currentView === 'inbox' ? 'active' : ''} type="button" onClick={openInbox}>依頼</button>
          <button className={currentView === 'audit' ? 'active' : ''} type="button" onClick={openAudit}>監査</button>
        </nav>
        <div className="topbar-actions">
          <span className="protected-badge"><ShieldIcon />予定詳細は保護されています</span>
          <div className="notification-wrap">
            <button className="notification-button" type="button" aria-label="通知" aria-expanded={notificationsOpen} onClick={openNotifications}>
              <span aria-hidden="true">●</span>
              {notifications.filter((item) => !item.readAt).length > 0 ? <b>{notifications.filter((item) => !item.readAt).length}</b> : null}
            </button>
            {notificationsOpen ? (
              <section className="notification-panel" aria-label="通知一覧">
                <div className="notification-heading"><strong>通知</strong><span>予定詳細は含まれません</span></div>
                {notificationsLoading ? <p role="status">取得中…</p> : null}
                {!notificationsLoading && notifications.length === 0 ? <p>新しい通知はありません。</p> : null}
                {notifications.map((item) => (
                  <button className={item.readAt ? 'notification-item read' : 'notification-item'} type="button" key={item.id} onClick={() => markNotificationRead(item.id)}>
                    <span>{item.message}</span>
                    <time>{formatDateTime(item.createdAt)}</time>
                  </button>
                ))}
              </section>
            ) : null}
          </div>
          <div className="account-wrap">
            <button className="avatar" type="button" aria-label="山田太郎のアカウントメニュー" aria-expanded={accountOpen} onClick={() => setAccountOpen(!accountOpen)}>山</button>
            {accountOpen ? (
              <div className="account-menu">
                <strong>山田 太郎</strong>
                <span>Manager · Asia/Tokyo</span>
                <button type="button" onClick={exportUserData} disabled={exporting}>{exporting ? '準備中…' : '本人データをエクスポート'}</button>
              </div>
            ) : null}
          </div>
        </div>
      </header>

      <main id="top">
        {currentView === 'calendar' ? (
        <>
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
              {displayedProjections.map((projection) => (
                <div className={`projection-row ${projection.tone}`} key={projection.time}>
                  <time>{projection.time}</time>
                  <strong>{projection.label}</strong>
                  <span className="state-dot" aria-hidden="true" />
                </div>
              ))}
              {memberPreview && displayedProjections.length === 0 ? (
                <p className="empty-state">この日に共有されている状態はありません。</p>
              ) : null}
            </div>
            <button className="preview-button" type="button" disabled={previewLoading} onClick={toggleMemberPreview}>
              {previewLoading ? '取得中…' : memberPreview ? '自分の表示に戻る' : 'メンバー表示をプレビュー'} <span aria-hidden="true">→</span>
            </button>
          </article>
        </section>
        </>
        ) : currentView === 'people' ? (
          <section className="people-view" aria-labelledby="people-title">
            <div className="people-heading">
              <div>
                <p className="eyebrow">PRODUCT STUDIO · PEOPLE</p>
                <h1 id="people-title">誰に、どう相談できるか。</h1>
                <p className="hero-copy">予定名ではなく、いま共有されている関わりやすさだけを表示します。</p>
              </div>
              <button className="secondary-button" type="button" onClick={openPeopleView}>更新</button>
            </div>
            {peopleLoading ? <p className="people-status" role="status">公開状態を取得しています…</p> : null}
            {peopleError ? <p className="people-status error" role="alert">{peopleError}</p> : null}
            {!peopleLoading && !peopleError && people.length === 0 ? (
              <p className="people-status">表示できる管理職はいません。</p>
            ) : null}
            <div className="people-list">
              {people.map((person) => (
                <article className="person-card" key={person.id}>
                  <div className="person-profile">
                    <span className="person-avatar">{person.displayName.slice(0, 1)}</span>
                    <div>
                      <h2>{person.displayName}</h2>
                      <p>{person.role} · {person.timezone}</p>
                    </div>
                    <button type="button" onClick={() => setActiveDialog('request')}>依頼を作成</button>
                  </div>
                  <div className="person-timeline" aria-label={`${person.displayName}の公開状態`}>
                    {person.segments.map((segment) => (
                      <div className={`person-segment ${segment.tone}`} key={`${person.id}-${segment.time}`}>
                        <time>{segment.time}</time>
                        <strong>{segment.label}</strong>
                      </div>
                    ))}
                    {person.segments.length === 0 ? <p>公開状態はありません。</p> : null}
                  </div>
                </article>
              ))}
            </div>
          </section>
        ) : currentView === 'inbox' ? (
          <section className="inbox-view" aria-labelledby="inbox-title">
            <div className="people-heading">
              <div>
                <p className="eyebrow">COORDINATION · INBOX</p>
                <h1 id="inbox-title">届いた依頼を、余白から選ぶ。</h1>
                <p className="hero-copy">予定の詳細を開かず、共有された候補と期限だけで判断できます。</p>
              </div>
              <button className="secondary-button" type="button" onClick={openInbox}>更新</button>
            </div>
            {inboxLoading ? <p className="people-status" role="status">依頼を取得しています…</p> : null}
            {inboxError ? <p className="people-status error" role="alert">{inboxError}</p> : null}
            {!inboxLoading && !inboxError && inboxRequests.length === 0 ? (
              <p className="people-status">新しい依頼はありません。</p>
            ) : null}
            <div className="request-list">
              {inboxRequests.map((item) => (
                <article className="request-card" key={item.id}>
                  <div className="request-summary">
                    <div className="request-meta">
                      <span className={`priority priority-${item.priority}`}>{item.priority}</span>
                      <span>{item.status}</span>
                    </div>
                    <h2>{item.title}</h2>
                    <p>{item.requesterUserId} · {item.durationMinutes}分 · 期限 {formatDateTime(item.deadlineAt)}</p>
                  </div>
                  <div className="option-list" aria-label={`${item.title}の候補`}>
                    {item.options.map((option) => (
                      <div className="option-row" key={option.id}>
                        <span>{option.type === 'meeting' ? 'MEETING' : 'ASYNC'}</span>
                        <strong>{option.startAt && option.endAt
                          ? `${formatDateTime(option.startAt)} — ${new Intl.DateTimeFormat('ja-JP', { hour: '2-digit', minute: '2-digit', hour12: false }).format(new Date(option.endAt))}`
                          : '非同期で回答'}</strong>
                        {item.status === 'suggested' ? (
                          <button type="button" disabled={respondingRequestID === item.id} onClick={() => respondToRequest(item.id, 'accept', option.id)}>この候補を承認</button>
                        ) : null}
                      </div>
                    ))}
                    {item.status === 'suggested' ? (
                      <>
                        <form className="suggest-form" onSubmit={(event) => suggestTime(event, item.id)}>
                          <label>別の開始時間<input name="suggestStart" type="datetime-local" required /></label>
                          <label>終了時間<input name="suggestEnd" type="datetime-local" required /></label>
                          <button type="submit" disabled={respondingRequestID === item.id}>別時間を提案</button>
                        </form>
                        <form className="delegate-form" onSubmit={(event) => delegateRequest(event, item.id)}>
                          <label>委譲先ユーザー<input name="delegateUserId" defaultValue="demo-member" required /></label>
                          <button type="submit" disabled={respondingRequestID === item.id}>委譲する</button>
                        </form>
                        <button className="decline-button" type="button" disabled={respondingRequestID === item.id} onClick={() => respondToRequest(item.id, 'decline')}>今回は辞退</button>
                      </>
                    ) : <p className="response-complete">回答済み · {item.status}</p>}
                  </div>
                </article>
              ))}
            </div>
          </section>
        ) : (
          <section className="audit-view" aria-labelledby="audit-title">
            <div className="people-heading">
              <div>
                <p className="eyebrow">PRIVACY · AUDIT LOG</p>
                <h1 id="audit-title">共有した事実だけを、記録する。</h1>
                <p className="hero-copy">操作主体・操作種別・依頼ID・時刻のみ。予定内容や依頼タイトルは記録されません。</p>
              </div>
              <button className="secondary-button" type="button" onClick={openAudit}>更新</button>
            </div>
            {auditLoading ? <p className="people-status" role="status">監査ログを取得しています…</p> : null}
            {auditError ? <p className="people-status error" role="alert">{auditError}</p> : null}
            {!auditLoading && !auditError && auditLogs.length === 0 ? <p className="people-status">監査ログはありません。</p> : null}
            <div className="audit-list">
              {auditLogs.map((event) => (
                <article className="audit-row" key={event.id}>
                  <time>{formatDateTime(event.createdAt)}</time>
                  <div>
                    <strong>{event.action.replaceAll('_', ' ')}</strong>
                    <span>{event.actorUserId} · {event.resourceType} · {event.resourceId}</span>
                  </div>
                  <span className="audit-safe"><ShieldIcon />予定詳細なし</span>
                </article>
              ))}
            </div>
          </section>
        )}
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
                <button className="primary-button" type="submit" disabled={requestSaving}>{requestSaving ? '送信中…' : '候補を生成して送信'}</button>
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
