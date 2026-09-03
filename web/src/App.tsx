import { type FormEvent, useEffect, useMemo, useState } from 'react'

const demoPrivateEvents = [
  { id: 'demo-1', time: '09:00', label: 'Product Review', size: 'short', details: [] as string[] },
  { id: 'demo-2', time: '10:00', label: 'Customer Meeting', size: 'medium', details: [] as string[] },
  { id: 'demo-3', time: '11:30', label: 'Focus', size: 'large', details: [] as string[] },
  { id: 'demo-4', time: '13:00', label: 'Recruiting Interview', size: 'large', details: [] as string[] },
]

const initialProjections = [
  { time: '09:00 — 10:00', label: '相談可能', tone: 'available' },
  { time: '10:00 — 11:30', label: '緊急のみ', tone: 'urgent' },
  { time: '11:30 — 13:00', label: '割り込み非推奨', tone: 'focus' },
  { time: '13:00 — 15:30', label: '対応困難', tone: 'unavailable' },
  { time: '15:30 —', label: '15分相談可能', tone: 'available' },
]

const apiURL = import.meta.env.VITE_API_URL || 'http://localhost:8080'
const apiFetch = (input: RequestInfo | URL, init?: RequestInit) => fetch(input, { ...init, credentials: 'include' })

type ProjectionRow = (typeof initialProjections)[number]

type CalendarView = 'day' | 'week' | 'month'

type PrivateCalendarEvent = {
  id: string
  title: string
  description?: string
  location?: string
  attendees?: string[]
  conferenceUrl?: string
  startAt?: string
  endAt?: string
  startDate?: string
  endDate?: string
  allDay: boolean
}

type PrivateCalendarResponse = {
  timezone: string
  events: PrivateCalendarEvent[]
}

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
  targetUserId: string
  title: string
  type: string
  durationMinutes: number
  deadlineAt: string
  priority: string
  status: string
  asyncMessage?: string
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

type AuthUser = {
  userId: string
  organizationId: string
  email: string
  displayName: string
  avatarUrl?: string
  role: string
}

type CalendarConnection = {
  grantedScopes: string[]
  connectedAt: string
  lastSyncedAt?: string
  lastAttemptAt?: string
  nextAttemptAt?: string
  lastErrorCode?: string
  reconnectRequired: boolean
}

type Workspace = { id: string; name: string; role: string }
type InvitationPreview = {
  invitationId: string
  organizationId: string
  organizationName: string
  role: string
  expiresAt: string
}

type InteractionState = {
  availability: 'available' | 'limited' | 'unavailable' | 'unknown'
  interruptibility: 'open' | 'normal' | 'urgent_only' | 'do_not_interrupt'
  requestability: 'open' | 'async_only' | 'later' | 'closed'
  reschedulability: 'high' | 'medium' | 'low' | 'fixed'
}

type SharingPolicyDraft = {
  default: InteractionState
  workingHours: Array<{ weekday: number; startMinute: number; endMinute: number }>
  rules: Array<{
    conditionType: string
    condition: unknown
    state: InteractionState
    priority: number
    enabled: boolean
  }>
}

const defaultSharingPolicy: SharingPolicyDraft = {
  default: {
    availability: 'available', interruptibility: 'normal',
    requestability: 'open', reschedulability: 'medium',
  },
  workingHours: [1, 2, 3, 4, 5].map((weekday) => ({ weekday, startMinute: 9 * 60, endMinute: 18 * 60 })),
  rules: [],
}

const stateForAvailability = (availability: InteractionState['availability']): InteractionState => {
  if (availability === 'available') {
    return { availability, interruptibility: 'normal', requestability: 'open', reschedulability: 'medium' }
  }
  if (availability === 'limited') {
    return { availability, interruptibility: 'urgent_only', requestability: 'later', reschedulability: 'low' }
  }
  if (availability === 'unavailable') {
    return { availability, interruptibility: 'do_not_interrupt', requestability: 'closed', reschedulability: 'fixed' }
  }
  return { availability, interruptibility: 'normal', requestability: 'later', reschedulability: 'low' }
}


const calendarRange = (anchor: Date, view: CalendarView) => {
  const from = new Date(anchor)
  from.setHours(0, 0, 0, 0)
  if (view === 'week') {
    const weekday = (from.getDay() + 6) % 7
    from.setDate(from.getDate() - weekday)
  } else if (view === 'month') {
    from.setDate(1)
  }
  const to = new Date(from)
  if (view === 'day') to.setDate(to.getDate() + 1)
  else if (view === 'week') to.setDate(to.getDate() + 7)
  else to.setMonth(to.getMonth() + 1)
  return { from, to }
}

const calendarRangeLabel = (from: Date, to: Date, view: CalendarView) => {
  if (view === 'day') {
    return new Intl.DateTimeFormat('ja-JP', { month: 'long', day: 'numeric', weekday: 'short' }).format(from)
  }
  if (view === 'month') {
    return new Intl.DateTimeFormat('ja-JP', { year: 'numeric', month: 'long' }).format(from)
  }
  const end = new Date(to)
  end.setDate(end.getDate() - 1)
  const formatter = new Intl.DateTimeFormat('ja-JP', { month: 'numeric', day: 'numeric' })
  return `${formatter.format(from)} — ${formatter.format(end)}`
}

const privateEventRow = (event: PrivateCalendarEvent, view: CalendarView) => {
  const start = event.startAt ? new Date(event.startAt) : null
  const end = event.endAt ? new Date(event.endAt) : null
  const timeFormatter = new Intl.DateTimeFormat('ja-JP', { hour: '2-digit', minute: '2-digit', hour12: false })
  const dateFormatter = new Intl.DateTimeFormat('ja-JP', { month: 'numeric', day: 'numeric' })
  const prefix = view === 'day' || !start ? '' : `${dateFormatter.format(start)} `
  const time = event.allDay ? `${event.startDate ?? ''} 終日` : start ? `${prefix}${timeFormatter.format(start)}` : '時刻未定'
  const minutes = start && end ? (end.getTime() - start.getTime()) / 60000 : 60
  const details = [event.location, ...(event.attendees ?? []), event.description, event.conferenceUrl].filter(Boolean) as string[]
  return {
    id: event.id, time, label: event.title || '(タイトルなし)',
    size: minutes >= 90 ? 'large' : minutes >= 45 ? 'medium' : 'short',
    details,
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
  const [calendarAnchor, setCalendarAnchor] = useState(() => new Date())
  const [calendarView, setCalendarView] = useState<CalendarView>('day')
  const [calendarLayer, setCalendarLayer] = useState<'both' | 'private' | 'projection'>('both')
  const [privateCalendarEvents, setPrivateCalendarEvents] = useState<PrivateCalendarEvent[]>([])
  const [privateEventsLoading, setPrivateEventsLoading] = useState(false)
  const [privateEventsError, setPrivateEventsError] = useState('')
  const [selectedPrivateEventID, setSelectedPrivateEventID] = useState('')
  const [projections, setProjections] = useState(initialProjections)
  const [memberProjections, setMemberProjections] = useState<ProjectionRow[]>([])
  const [previewLoading, setPreviewLoading] = useState(false)
  const [currentView, setCurrentView] = useState<'calendar' | 'people' | 'inbox' | 'sent' | 'audit'>('calendar')
  const [people, setPeople] = useState<PersonCard[]>([])
  const [peopleLoading, setPeopleLoading] = useState(false)
  const [peopleError, setPeopleError] = useState('')
  const [requestSaving, setRequestSaving] = useState(false)
  const [inboxRequests, setInboxRequests] = useState<CoordinationRequest[]>([])
  const [inboxLoading, setInboxLoading] = useState(false)
  const [inboxError, setInboxError] = useState('')
  const [sentRequests, setSentRequests] = useState<CoordinationRequest[]>([])
  const [sentLoading, setSentLoading] = useState(false)
  const [sentError, setSentError] = useState('')
  const [respondingRequestID, setRespondingRequestID] = useState('')
  const [auditLogs, setAuditLogs] = useState<AuditEvent[]>([])
  const [auditLoading, setAuditLoading] = useState(false)
  const [auditError, setAuditError] = useState('')
  const [overrideSaving, setOverrideSaving] = useState(false)
  const [exporting, setExporting] = useState(false)
  const [deletingAccount, setDeletingAccount] = useState(false)
  const [policyLoading, setPolicyLoading] = useState(false)
  const [policySaving, setPolicySaving] = useState(false)
  const [sharingPolicy, setSharingPolicy] = useState<SharingPolicyDraft>(defaultSharingPolicy)
  const [authUser, setAuthUser] = useState<AuthUser | null>(null)
  const [demoMode, setDemoMode] = useState(import.meta.env.DEV)
  const [calendarConnection, setCalendarConnection] = useState<CalendarConnection | null>(null)
  const [calendarBusy, setCalendarBusy] = useState(false)
  const [workspaces, setWorkspaces] = useState<Workspace[]>([])
  const [invitationToken, setInvitationToken] = useState('')
  const [invitationPreview, setInvitationPreview] = useState<InvitationPreview | null>(null)
  const [inviteRole, setInviteRole] = useState('MEMBER')
  const [inviteURL, setInviteURL] = useState('')
  const [workspaceBusy, setWorkspaceBusy] = useState(false)
  const visibleRange = useMemo(() => calendarRange(calendarAnchor, calendarView), [calendarAnchor, calendarView])
  const visibleDate = calendarAnchor
  const dateLabel = calendarRangeLabel(visibleRange.from, visibleRange.to, calendarView)
  const displayedProjections = memberPreview ? memberProjections : projections
  const activeUserID = authUser?.userId ?? 'demo-manager'
  const activeOrganizationID = authUser?.organizationId ?? 'demo-org'
  const requesterUserID = authUser?.userId ?? 'demo-member'

  useEffect(() => {
    const params = new URLSearchParams(window.location.search)
    const authCompleted = params.get('auth') === 'success'
    const calendarCompleted = params.get('calendar') === 'connected'
    const incomingInvitation = params.get('invite') ?? ''
    if (!authCompleted && !calendarCompleted && !incomingInvitation && import.meta.env.DEV) return
    const loadSession = async () => {
      try {
        const response = await apiFetch(`${apiURL}/api/v1/auth/session`)
        if (!response.ok) throw new Error('session failed')
        const payload = await response.json() as { authenticated: boolean; demoMode?: boolean; user?: AuthUser }
        setDemoMode(payload.demoMode === true)
        if (payload.authenticated && payload.user) {
          setAuthUser(payload.user)
          if (authCompleted) setNotice('Googleアカウントでログインしました。')
          if (calendarCompleted) setNotice('Google Calendarを接続しました。同期を開始できます。')
          const calendarResponse = await apiFetch(`${apiURL}/api/v1/calendar/connection`)
          if (calendarResponse.ok) {
            const calendarPayload = await calendarResponse.json() as { connected: boolean; connection?: CalendarConnection }
            setCalendarConnection(calendarPayload.connected ? calendarPayload.connection ?? null : null)
          }
          const workspaceResponse = await apiFetch(`${apiURL}/api/v1/workspaces`)
          if (workspaceResponse.ok) {
            const workspacePayload = await workspaceResponse.json() as { workspaces: Workspace[] }
            setWorkspaces(workspacePayload.workspaces)
          }
          if (incomingInvitation) {
            setInvitationToken(incomingInvitation)
            const invitationResponse = await apiFetch(`${apiURL}/api/v1/invitations/preview`, {
              method: 'POST', headers: { 'Content-Type': 'application/json' },
              body: JSON.stringify({ token: incomingInvitation }),
            })
            if (invitationResponse.ok) setInvitationPreview(await invitationResponse.json() as InvitationPreview)
            else setNotice('招待リンクは無効、期限切れ、または使用済みです。')
          }
        }
      } catch {
        if (authCompleted || calendarCompleted) setNotice('ログイン状態を確認できませんでした。')
      } finally {
        if (authCompleted || calendarCompleted || incomingInvitation) window.history.replaceState({}, '', window.location.pathname)
      }
    }
    void loadSession()
  }, [])


  useEffect(() => {
    if (!authUser || !calendarConnection || currentView !== 'calendar') return
    let cancelled = false
    const load = async () => {
      setPrivateEventsLoading(true)
      setPrivateEventsError('')
      const query = new URLSearchParams({ from: visibleRange.from.toISOString(), to: visibleRange.to.toISOString() })
      try {
        const [privateResponse, projectionResponse] = await Promise.all([
          apiFetch(`${apiURL}/api/v1/me/private-events?${query}`),
          apiFetch(`${apiURL}/api/v1/people/${authUser.userId}/projection?timezone=Asia%2FTokyo&${query}`, {
            headers: { 'X-Demo-User-ID': authUser.userId, 'X-Organization-ID': authUser.organizationId },
          }),
        ])
        if (!privateResponse.ok) {
          throw new Error(privateResponse.status === 409 ? 'reconnect' : 'private')
        }
        const privatePayload = await privateResponse.json() as PrivateCalendarResponse
        if (!cancelled) setPrivateCalendarEvents(privatePayload.events ?? [])
        if (projectionResponse.ok) {
          const projectionPayload = await projectionResponse.json() as PublicProjection
          if (!cancelled) setProjections(mapPublicSegments(projectionPayload))
        }
      } catch (error) {
        if (!cancelled) {
          setPrivateCalendarEvents([])
          setPrivateEventsError(error instanceof Error && error.message === 'reconnect'
            ? 'Google Calendarの再接続が必要です。'
            : '本人用カレンダーを取得できませんでした。')
        }
      } finally {
        if (!cancelled) setPrivateEventsLoading(false)
      }
    }
    void load()
    return () => { cancelled = true }
  }, [authUser, calendarConnection, currentView, visibleRange])

  const moveCalendar = (direction: -1 | 1) => {
    setCalendarAnchor((current) => {
      const next = new Date(current)
      if (calendarView === 'month') next.setMonth(next.getMonth() + direction)
      else next.setDate(next.getDate() + direction * (calendarView === 'week' ? 7 : 1))
      return next
    })
    setSelectedPrivateEventID('')
  }

  const privateRows = authUser
    ? privateCalendarEvents.map((event) => privateEventRow(event, calendarView))
    : demoMode ? demoPrivateEvents : []

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
      const response = await apiFetch(`${apiURL}/api/v1/requests`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'X-Demo-User-ID': requesterUserID,
          'X-Organization-ID': activeOrganizationID,
        },
        body: JSON.stringify({
          targetUserId: authUser ? activeUserID : 'demo-manager',
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
      const created = await response.json() as CoordinationRequest
      const optionCount = Array.isArray(created.options) ? created.options.length : 0
      setSentRequests((current) => [created, ...current.filter((item) => item.id !== created.id)])
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
      const response = await apiFetch(`${apiURL}/api/v1/users/${activeUserID}/manual-overrides`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-Demo-User-ID': activeUserID },
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
      const response = await apiFetch(`${apiURL}/api/v1/people/${activeUserID}/projection?${query}`, {
        headers: { 'X-Demo-User-ID': activeUserID, 'X-Organization-ID': activeOrganizationID },
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

  const openPeopleView = async () => {
    setCurrentView('people')
    setPeopleLoading(true)
    setPeopleError('')
    try {
      const response = await apiFetch(`${apiURL}/api/v1/people?organizationId=${activeOrganizationID}`, {
        headers: { 'X-Demo-User-ID': activeUserID, 'X-Organization-ID': activeOrganizationID },
      })
      if (!response.ok) {
        throw new Error('people failed')
      }
      const directory = await response.json() as { people: Array<Omit<PersonCard, 'segments'>> }
      const cards = await Promise.all(directory.people.map(async (person) => {
        const projectionResponse = await apiFetch(
          `${apiURL}/api/v1/people/${person.id}/projection?timezone=${encodeURIComponent(person.timezone)}`,
          { headers: { 'X-Demo-User-ID': activeUserID, 'X-Organization-ID': activeOrganizationID } },
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
      const response = await apiFetch(`${apiURL}/api/v1/requests`, {
        headers: { 'X-Demo-User-ID': activeUserID },
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

  const openSentRequests = async () => {
    setCurrentView('sent')
    setSentLoading(true)
    setSentError('')
    try {
      const response = await apiFetch(`${apiURL}/api/v1/requests?scope=sent`, {
        headers: { 'X-Demo-User-ID': requesterUserID },
      })
      if (!response.ok) throw new Error('sent requests failed')
      const payload = await response.json() as { requests: CoordinationRequest[] }
      setSentRequests(payload.requests)
    } catch {
      setSentError('送信済み依頼を取得できませんでした。')
    } finally {
      setSentLoading(false)
    }
  }

  const cancelSentRequest = async (requestID: string) => {
    setRespondingRequestID(requestID)
    try {
      const response = await apiFetch(`${apiURL}/api/v1/requests/${requestID}/cancel`, {
        method: 'POST', headers: { 'X-Demo-User-ID': requesterUserID },
      })
      if (!response.ok) throw new Error('request cancellation failed')
      setSentRequests((current) => current.map((item) => item.id === requestID
        ? { ...item, status: 'cancelled' }
        : item))
      setNotice('依頼をキャンセルしました。相手にも通知しました。')
    } catch {
      setNotice('依頼をキャンセルできませんでした。最新状態を確認してください。')
    } finally {
      setRespondingRequestID('')
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
      const response = await apiFetch(`${apiURL}/api/v1/notifications`, {
        headers: { 'X-Demo-User-ID': activeUserID },
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
      const response = await apiFetch(`${apiURL}/api/v1/notifications/${id}/read`, {
        method: 'POST', headers: { 'X-Demo-User-ID': activeUserID },
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
      const response = await apiFetch(`${apiURL}/api/v1/audit-logs`, {
        headers: {
          'X-Demo-User-ID': activeUserID,
          'X-Organization-ID': activeOrganizationID,
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
      const response = await apiFetch(`${apiURL}/api/v1/requests/${requestID}/${action}`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'X-Demo-User-ID': activeUserID,
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

  const respondAsync = async (event: FormEvent<HTMLFormElement>, requestID: string) => {
    event.preventDefault()
    const formElement = event.currentTarget
    const message = String(new FormData(formElement).get('asyncMessage')).trim()
    setRespondingRequestID(requestID)
    try {
      const response = await apiFetch(`${apiURL}/api/v1/requests/${requestID}/async`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'X-Demo-User-ID': activeUserID,
        },
        body: JSON.stringify({ message }),
      })
      if (!response.ok) throw new Error('async response failed')
      const payload = await response.json() as { status: string; asyncMessage: string }
      setInboxRequests((current) => current.map((item) => item.id === requestID
        ? { ...item, status: payload.status, asyncMessage: payload.asyncMessage }
        : item))
      setNotice('非同期で回答しました。依頼者に通知しました。')
      formElement.reset()
    } catch {
      setNotice('非同期で回答できませんでした。最新状態とメッセージを確認してください。')
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
      const response = await apiFetch(`${apiURL}/api/v1/requests/${requestID}/suggest`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'X-Demo-User-ID': activeUserID,
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
      const response = await apiFetch(`${apiURL}/api/v1/requests/${requestID}/delegate`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'X-Demo-User-ID': activeUserID,
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
      const response = await apiFetch(`${apiURL}/api/v1/users/${activeUserID}/export`, {
        headers: { 'X-Demo-User-ID': activeUserID },
      })
      if (!response.ok) throw new Error('export failed')
      const blob = await response.blob()
      const url = URL.createObjectURL(blob)
      const link = document.createElement('a')
      link.href = url
      link.download = `negotiable-calendar-${activeUserID}-${new Date().toISOString().slice(0, 10)}.json`
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

  const deleteAccount = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    const confirmation = String(new FormData(event.currentTarget).get('confirmation'))
    setDeletingAccount(true)
    try {
      const response = await apiFetch(`${apiURL}/api/v1/me/account`, {
        method: 'DELETE',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ confirmation }),
      })
      if (response.status === 409) {
        setNotice('共有Workspaceの最後のOWNERです。先に別のOWNERへ引き継いでください。')
        return
      }
      if (!response.ok) throw new Error('account deletion failed')
      setAuthUser(null)
      setCalendarConnection(null)
      setWorkspaces([])
      setPrivateCalendarEvents([])
      setProjections([])
      setActiveDialog('')
      setAccountOpen(false)
      setNotice('アカウントと保存データを削除しました。')
    } catch {
      setNotice('アカウントを削除できませんでした。時間をおいて再試行してください。')
    } finally {
      setDeletingAccount(false)
    }
  }

  const openSharingRules = async () => {
    setActiveDialog('rules')
    setPolicyLoading(true)
    try {
      const response = await apiFetch(`${apiURL}/api/v1/users/${activeUserID}/sharing-policy`, {
        headers: { 'X-Demo-User-ID': activeUserID },
      })
      if (response.status === 404) {
        setSharingPolicy(defaultSharingPolicy)
        return
      }
      if (!response.ok) throw new Error('policy load failed')
      const value = await response.json() as SharingPolicyDraft
      setSharingPolicy({
        default: value.default ?? defaultSharingPolicy.default,
        workingHours: value.workingHours?.length ? value.workingHours : defaultSharingPolicy.workingHours,
        rules: (value.rules ?? []).map(({ conditionType, condition, state, priority, enabled }) => ({
          conditionType, condition, state, priority, enabled,
        })),
      })
    } catch {
      setNotice('共有ルールを取得できませんでした。')
    } finally {
      setPolicyLoading(false)
    }
  }

  const saveSharingRules = async () => {
    setPolicySaving(true)
    try {
      const response = await apiFetch(`${apiURL}/api/v1/users/${activeUserID}/sharing-policy`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json', 'X-Demo-User-ID': activeUserID },
        body: JSON.stringify(sharingPolicy),
      })
      if (!response.ok) throw new Error('policy save failed')
      const value = await response.json() as SharingPolicyDraft
      setSharingPolicy({
        default: value.default,
        workingHours: value.workingHours,
        rules: value.rules.map(({ conditionType, condition, state, priority, enabled }) => ({
          conditionType, condition, state, priority, enabled,
        })),
      })
      setActiveDialog('')
      setNotice('共有ルールを保存しました。')
    } catch {
      setNotice('共有ルールを保存できませんでした。入力内容を確認してください。')
    } finally {
      setPolicySaving(false)
    }
  }

  const logout = async () => {
    try {
      const response = await apiFetch(`${apiURL}/api/v1/auth/logout`, {
        method: 'POST',
      })
      if (!response.ok) throw new Error('logout failed')
      setAuthUser(null)
      setCalendarConnection(null)
      setAccountOpen(false)
      setNotice('ログアウトしました。')
    } catch {
      setNotice('ログアウトできませんでした。')
    }
  }

  const syncCalendar = async () => {
    setCalendarBusy(true)
    try {
      const response = await apiFetch(`${apiURL}/api/v1/calendar/sync`, { method: 'POST' })
      if (!response.ok) throw new Error('sync failed')
      const payload = await response.json() as { busySpanCount: number; lastSyncedAt: string }
      setCalendarConnection((current) => current ? { ...current, lastSyncedAt: payload.lastSyncedAt, reconnectRequired: false } : current)
      setNotice(`Google Calendarから${payload.busySpanCount}件のbusy時間を同期しました。予定名は保存していません。`)
      try {
        const from = new Date(visibleDate)
        from.setHours(0, 0, 0, 0)
        const to = new Date(from)
        to.setDate(to.getDate() + 1)
        const query = new URLSearchParams({
          timezone: 'Asia/Tokyo',
          from: from.toISOString(),
          to: to.toISOString(),
        })
        const projectionResponse = await apiFetch(`${apiURL}/api/v1/people/${activeUserID}/projection?${query}`, {
          headers: { 'X-Demo-User-ID': activeUserID, 'X-Organization-ID': activeOrganizationID },
        })
        if (projectionResponse.ok) {
          const view = await projectionResponse.json() as PublicProjection
          setProjections(mapPublicSegments(view))
        }
      } catch {
        setNotice(`Google Calendarから${payload.busySpanCount}件を同期しましたが、表示の再読込に失敗しました。`)
      }
    } catch {
      setNotice('Calendarを同期できませんでした。再接続が必要な場合があります。')
    } finally {
      setCalendarBusy(false)
    }
  }

  const disconnectCalendar = async () => {
    setCalendarBusy(true)
    try {
      const response = await apiFetch(`${apiURL}/api/v1/calendar/connection`, { method: 'DELETE' })
      if (!response.ok) throw new Error('disconnect failed')
      setCalendarConnection(null)
      setNotice('Google Calendarの接続と同期済みbusy時間を削除しました。')
    } catch {
      setNotice('Calendar接続を解除できませんでした。')
    } finally {
      setCalendarBusy(false)
    }
  }

  const createInvitation = async () => {
    setWorkspaceBusy(true)
    try {
      const response = await apiFetch(`${apiURL}/api/v1/workspaces/${activeOrganizationID}/invitations`, {
        method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ role: inviteRole }),
      })
      if (!response.ok) throw new Error('invite failed')
      const payload = await response.json() as { inviteUrl: string }
      setInviteURL(payload.inviteUrl)
      try { await navigator.clipboard?.writeText(payload.inviteUrl) } catch { /* link remains visible for manual copy */ }
      setNotice('一回限りの招待リンクを作成しました。')
    } catch { setNotice('招待リンクを作成できませんでした。権限を確認してください。') }
    finally { setWorkspaceBusy(false) }
  }

  const switchWorkspace = async (organizationId: string) => {
    if (organizationId === activeOrganizationID) return
    setWorkspaceBusy(true)
    try {
      const response = await apiFetch(`${apiURL}/api/v1/workspaces/switch`, {
        method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ organizationId }),
      })
      if (!response.ok) throw new Error('switch failed')
      const payload = await response.json() as { activeWorkspace: Workspace }
      setAuthUser((current) => current ? { ...current, organizationId: payload.activeWorkspace.id, role: payload.activeWorkspace.role } : current)
      setNotice(`${payload.activeWorkspace.name} に切り替えました。`)
    } catch { setNotice('Workspaceを切り替えられませんでした。') }
    finally { setWorkspaceBusy(false) }
  }

  const acceptInvitation = async () => {
    if (!invitationPreview || !invitationToken) return
    setWorkspaceBusy(true)
    try {
      const accepted = await apiFetch(`${apiURL}/api/v1/invitations/accept`, {
        method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ token: invitationToken }),
      })
      if (!accepted.ok) throw new Error('accept failed')
      const switched = await apiFetch(`${apiURL}/api/v1/workspaces/switch`, {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ organizationId: invitationPreview.organizationId }),
      })
      if (!switched.ok) throw new Error('switch failed')
      const payload = await switched.json() as { activeWorkspace: Workspace }
      setAuthUser((current) => current ? { ...current, organizationId: payload.activeWorkspace.id, role: payload.activeWorkspace.role } : current)
      setWorkspaces((current) => [...current.filter((item) => item.id !== payload.activeWorkspace.id), payload.activeWorkspace])
      setInvitationPreview(null)
      setInvitationToken('')
      setNotice(`${payload.activeWorkspace.name} に参加しました。`)
    } catch { setNotice('招待を受諾できませんでした。') }
    finally { setWorkspaceBusy(false) }
  }

  if (!authUser && !demoMode) {
    return (
      <div className="app-shell">
        <main className="signin-gate">
          <span className="brand-mark" aria-hidden="true">N</span>
          <p className="eyebrow">NEGOTIABLE CALENDAR</p>
          <h1>予定を見せずに、予定を共有する。</h1>
          <p className="hero-copy">本人確認後に、あなたのカレンダーと組織の調整状態を表示します。</p>
          {notice ? <p role="status">{notice}</p> : null}
          <a className="primary-button" href={`${apiURL}/api/v1/auth/google/login`}>Googleでログイン</a>
        </main>
      </div>
    )
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
          <button className={currentView === 'sent' ? 'active' : ''} type="button" onClick={openSentRequests}>送信済み</button>
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
                <strong>{authUser?.displayName ?? '山田 太郎'}</strong>
                <span>{authUser ? `${authUser.role} · ${authUser.email}` : 'Manager · Demo mode'}</span>
                {authUser ? (
                  <>
                    {workspaces.length > 0 ? (
                      <select aria-label="Workspace" value={activeOrganizationID} disabled={workspaceBusy} onChange={(event) => void switchWorkspace(event.target.value)}>
                        {workspaces.map((workspace) => <option key={workspace.id} value={workspace.id}>{workspace.name} · {workspace.role}</option>)}
                      </select>
                    ) : null}
                    {authUser.role === 'OWNER' || authUser.role === 'ADMIN' ? (
                      <div className="invite-controls">
                        <select aria-label="招待する役割" value={inviteRole} onChange={(event) => setInviteRole(event.target.value)}>
                          {authUser.role === 'OWNER' ? <option value="ADMIN">ADMIN</option> : null}
                          <option value="MANAGER">MANAGER</option>
                          <option value="MEMBER">MEMBER</option>
                        </select>
                        <button type="button" onClick={createInvitation} disabled={workspaceBusy}>招待リンクを作成</button>
                        {inviteURL ? <input aria-label="招待リンク" readOnly value={inviteURL} /> : null}
                      </div>
                    ) : null}
                    {calendarConnection ? (
                      <>
                        <span>{calendarConnection.reconnectRequired ? 'Calendarの再接続が必要です' : 'Calendar 自動同期中'}{calendarConnection.lastSyncedAt ? ` · 最終成功 ${formatDateTime(calendarConnection.lastSyncedAt)}` : ''}</span>
                        {!calendarConnection.reconnectRequired && calendarConnection.lastErrorCode ? (
                          <span role="status">前回の自動同期に失敗しました（{calendarConnection.lastErrorCode}）。{calendarConnection.nextAttemptAt ? `次回 ${formatDateTime(calendarConnection.nextAttemptAt)}` : '自動で再試行します。'}</span>
                        ) : null}
                        {!calendarConnection.reconnectRequired && !calendarConnection.lastErrorCode && calendarConnection.nextAttemptAt ? (
                          <span>次回の自動同期 {formatDateTime(calendarConnection.nextAttemptAt)}</span>
                        ) : null}
                        {calendarConnection.reconnectRequired ? (
                          <a href={`${apiURL}/api/v1/calendar/google/connect`}>Google Calendarを再接続</a>
                        ) : (
                          <button type="button" onClick={syncCalendar} disabled={calendarBusy}>{calendarBusy ? '処理中…' : 'busy時間を同期'}</button>
                        )}
                        <button type="button" onClick={disconnectCalendar} disabled={calendarBusy}>Calendar接続を解除</button>
                      </>
                    ) : (
                      <a href={`${apiURL}/api/v1/calendar/google/connect`}>Google Calendarを接続</a>
                    )}
                    <button type="button" onClick={logout}>ログアウト</button>
                    <button className="danger-link" type="button" onClick={() => { setAccountOpen(false); setActiveDialog('delete-account') }}>アカウントを削除</button>
                  </>
                ) : (
                  <a href={`${apiURL}/api/v1/auth/google/login`}>Googleでログイン</a>
                )}
                <button type="button" onClick={exportUserData} disabled={exporting}>{exporting ? '準備中…' : '本人データをエクスポート'}</button>
              </div>
            ) : null}
          </div>
        </div>
      </header>

      <main id="top">
        {invitationPreview ? (
          <section className="privacy-note" aria-label="Workspace招待">
            <ShieldIcon />
            <div><strong>{invitationPreview.organizationName} への招待</strong><span>付与される役割: {invitationPreview.role}</span></div>
            <button type="button" onClick={acceptInvitation} disabled={workspaceBusy}>{workspaceBusy ? '参加中…' : '招待を受諾'}</button>
          </section>
        ) : null}
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
          <button type="button" onClick={openSharingRules}>共有ルールを確認</button>
        </section>

        <section className="calendar-toolbar" aria-label="カレンダー操作">
          <div className="date-controls">
            <button type="button" aria-label={calendarView === 'day' ? '前の日' : calendarView === 'week' ? '前の週' : '前の月'} onClick={() => moveCalendar(-1)}>←</button>
            <button type="button" onClick={() => setCalendarAnchor(new Date())}>今日</button>
            <button type="button" aria-label={calendarView === 'day' ? '次の日' : calendarView === 'week' ? '次の週' : '次の月'} onClick={() => moveCalendar(1)}>→</button>
          </div>
          <div className="calendar-view-controls" aria-label="表示期間">
            {(['day', 'week', 'month'] as CalendarView[]).map((view) => (
              <button className={calendarView === view ? 'active' : ''} type="button" key={view} onClick={() => setCalendarView(view)}>
                {view === 'day' ? '日' : view === 'week' ? '週' : '月'}
              </button>
            ))}
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
              {privateEventsLoading ? <p className="empty-state" role="status">本人用カレンダーを取得中…</p> : null}
              {privateEventsError ? <p className="empty-state error" role="alert">{privateEventsError}</p> : null}
              {!privateEventsLoading && !privateEventsError && privateRows.length === 0 ? <p className="empty-state">この期間の予定はありません。</p> : null}
              {privateRows.map((event) => (
                <div className="private-row" key={event.id ?? `${event.time}-${event.label}`}>
                  <time>{event.time}</time>
                  <button className={`private-event ${event.size}`} type="button" aria-expanded={selectedPrivateEventID === event.id} onClick={() => setSelectedPrivateEventID((current) => current === event.id ? '' : event.id)}>
                    <strong>{event.label}</strong>
                    <span>本人だけに表示 · 組織には非公開</span>
                    {selectedPrivateEventID === event.id && event.details?.length ? (
                      <span className="private-event-details">{event.details.join(' · ')}</span>
                    ) : null}
                  </button>
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
                        {item.status === 'suggested' && option.type === 'meeting' ? (
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
                        <form className="async-form" onSubmit={(event) => respondAsync(event, item.id)}>
                          <label>非同期メッセージ<textarea name="asyncMessage" maxLength={500} rows={2} placeholder="回答方法や次のアクションを500文字以内で入力" required /></label>
                          <button type="submit" disabled={respondingRequestID === item.id}>非同期で回答</button>
                        </form>
                        <form className="delegate-form" onSubmit={(event) => delegateRequest(event, item.id)}>
                          <label>委譲先ユーザー<input name="delegateUserId" defaultValue="demo-member" required /></label>
                          <button type="submit" disabled={respondingRequestID === item.id}>委譲する</button>
                        </form>
                        <button className="decline-button" type="button" disabled={respondingRequestID === item.id} onClick={() => respondToRequest(item.id, 'decline')}>今回は辞退</button>
                      </>
                    ) : <div><p className="response-complete">回答済み · {item.status}</p>{item.asyncMessage ? <p className="async-message">{item.asyncMessage}</p> : null}</div>}
                  </div>
                </article>
              ))}
            </div>
          </section>
        ) : currentView === 'sent' ? (
          <section className="inbox-view" aria-labelledby="sent-title">
            <div className="people-heading">
              <div>
                <p className="eyebrow">COORDINATION · SENT</p>
                <h1 id="sent-title">送った依頼を、最後まで管理する。</h1>
                <p className="hero-copy">依頼者本人だけが送信状況を確認し、回答前の依頼をキャンセルできます。</p>
              </div>
              <button className="secondary-button" type="button" onClick={openSentRequests}>更新</button>
            </div>
            {sentLoading ? <p className="people-status" role="status">送信済み依頼を取得しています…</p> : null}
            {sentError ? <p className="people-status error" role="alert">{sentError}</p> : null}
            {!sentLoading && !sentError && sentRequests.length === 0 ? <p className="people-status">送信済み依頼はありません。</p> : null}
            <div className="request-list">
              {sentRequests.map((item) => {
                const cancellable = ['pending', 'suggested', 'delegated'].includes(item.status)
                return (
                  <article className="request-card" key={item.id}>
                    <div className="request-summary">
                      <div className="request-meta">
                        <span className={`priority priority-${item.priority}`}>{item.priority}</span>
                        <span>{item.status}</span>
                      </div>
                      <h2>{item.title}</h2>
                      <p>{item.targetUserId} 宛 · {item.durationMinutes}分 · 期限 {formatDateTime(item.deadlineAt)}</p>
                    </div>
                    <div className="option-list">
                      <strong>{item.options.length}件の調整候補</strong>
                      {cancellable ? (
                        <button className="decline-button" type="button" disabled={respondingRequestID === item.id} onClick={() => cancelSentRequest(item.id)}>
                          {respondingRequestID === item.id ? 'キャンセル中…' : '依頼をキャンセル'}
                        </button>
                      ) : <div><p className="response-complete">更新済み · {item.status}</p>{item.asyncMessage ? <p className="async-message">{item.asyncMessage}</p> : null}</div>}
                    </div>
                  </article>
                )
              })}
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
              <div>
                <label htmlFor="default-availability">基本の公開状態</label>
                <select
                  id="default-availability"
                  value={sharingPolicy.default.availability}
                  disabled={policyLoading || policySaving}
                  onChange={(event) => setSharingPolicy((current) => ({
                    ...current,
                    default: stateForAvailability(event.target.value as InteractionState['availability']),
                  }))}
                >
                  <option value="available">相談可能</option>
                  <option value="limited">緊急のみ</option>
                  <option value="unavailable">対応不可</option>
                  <option value="unknown">要確認</option>
                </select>
              </div>
              <div><span>勤務時間</span><strong>平日 09:00 — 18:00</strong></div>
              <div><span>予定中</span><strong>緊急のみ</strong></div>
              <div><span>集中時間</span><strong>割り込み非推奨</strong></div>
              <div><span>空き時間</span><strong>相談可能</strong></div>
              <div><span>勤務時間外</span><strong>対応不可</strong></div>
            </div>
            <button className="primary-button full-button" type="button" onClick={saveSharingRules} disabled={policyLoading || policySaving}>{policyLoading ? '読み込み中…' : policySaving ? '保存中…' : 'このルールを保存'}</button>
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

      {activeDialog === 'delete-account' ? (
        <div className="modal-backdrop" role="presentation">
          <section className="modal danger-modal" role="dialog" aria-modal="true" aria-labelledby="delete-account-title">
            <div className="modal-heading">
              <div>
                <p className="eyebrow">DANGER ZONE</p>
                <h2 id="delete-account-title">アカウントを完全に削除</h2>
              </div>
              <button className="close-button" type="button" aria-label="閉じる" disabled={deletingAccount} onClick={() => setActiveDialog('')}>×</button>
            </div>
            <p className="modal-copy">この操作は取り消せません。Calendar連携を失効し、予定の投影、共有ルール、依頼、通知、セッションを削除します。</p>
            <form className="request-form" onSubmit={deleteAccount}>
              <label>
                確認のため DELETE と入力
                <input name="confirmation" autoComplete="off" pattern="DELETE" maxLength={6} required />
              </label>
              <div className="modal-actions">
                <button className="secondary-button" type="button" disabled={deletingAccount} onClick={() => setActiveDialog('')}>キャンセル</button>
                <button className="danger-button" type="submit" disabled={deletingAccount}>{deletingAccount ? '削除中…' : '完全に削除する'}</button>
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
