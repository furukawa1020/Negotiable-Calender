import { type FormEvent, useEffect, useMemo, useRef, useState } from 'react'
import SharingPolicyEditor from './SharingPolicyEditor'
import { ConfirmedMeeting } from './ConfirmedMeeting'
import { CalendarSyncStatus } from './CalendarSyncStatus'
import { CalendarConsent, CalendarConnectionHelp } from './CalendarConsent'
import { PolicyLinks } from './PolicyLinks'
import { calendarConsentNotice } from './calendarConsentNotice'
import { AccountAvatar } from './AccountAvatar'
import { RequestComposer } from './RequestComposer'
import { RequestHandoff } from './RequestHandoff'
import { CounterproposalForm, CounterproposalAgreement } from './Counterproposal'
import { useRequestResolution } from './useRequestResolution'
import { RescheduleMeeting } from './RescheduleMeeting'
import { LocalPlanning } from './LocalPlanningPanel'
import { PlanningRateLimitError } from './localPlanning'
import type { RescheduleCommand, RescheduleProposal } from './RescheduleMeeting'
import { sharingPolicyError, type SharingPolicyDraft } from './sharingPolicy'

const defaultSharingPolicy: SharingPolicyDraft = {
  default: {
    availability: 'available', interruptibility: 'normal',
    requestability: 'open', reschedulability: 'medium',
  },
  workingHours: [1, 2, 3, 4, 5].map((weekday) => ({ weekday, startMinute: 9 * 60, endMinute: 18 * 60 })),
  rules: [],
}

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

const apiURL = import.meta.env.VITE_API_URL || (import.meta.env.DEV ? 'http://localhost:8080' : window.location.origin)
const apiFetch = (input: RequestInfo | URL, init?: RequestInit) => fetch(input, { ...init, credentials: 'include' })

const requestStatusLabel = (status: string) => ({ pending: '未回答', suggested: '候補を確認中', accepted: '日時確定', declined: '辞退', delegated: '委譲済み', async: '非同期で回答', cancelled: 'キャンセル済み', expired: '期限切れ' }[status] ?? status)
const priorityLabel = (priority: string) => ({ normal: '通常', high: '重要', urgent: '緊急' }[priority] ?? priority)

function NavigationIcon({ view }: { view: string }) {
  const paths: Record<string, string> = {
    calendar: 'M5 3v4M15 3v4M3 9h14M4 5h12a1 1 0 0 1 1 1v11H3V6a1 1 0 0 1 1-1Z',
    people: 'M8 10a3 3 0 1 0 0-6 3 3 0 0 0 0 6ZM2 17v-2a4 4 0 0 1 4-4h4a4 4 0 0 1 4 4v2M14 4a3 3 0 0 1 0 6M16 12a3 3 0 0 1 2 3v2',
    inbox: 'M3 4h14v13H3V4ZM3 12h4l1 2h4l1-2h4M10 6v5M7 8l3 3 3-3',
    sent: 'M3 3l15 7-15 7 3-7-3-7ZM6 10h12',
    audit: 'M5 3h10v14H5V3ZM8 7h4M8 10h4M8 13h4',
  }
  return <svg viewBox="0 0 20 20" fill="none" stroke="currentColor" strokeWidth="1.5" aria-hidden="true"><path d={paths[view]} /></svg>
}

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
  proposedByUserId?: string
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
  delegatedFromUserId?: string
  acceptedOptionId?: string
  rescheduleProposal?: RescheduleProposal
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
  sourceFresh?: boolean
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
  const [requestTargetID, setRequestTargetID] = useState('')
  const [memberPreview, setMemberPreview] = useState(false)
  const [accountOpen, setAccountOpen] = useState(false)
  const [notificationsOpen, setNotificationsOpen] = useState(false)
  const [notifications, setNotifications] = useState<AppNotification[]>([])
  const [notificationsLoading, setNotificationsLoading] = useState(false)
  const [notice, setNotice] = useState(() => calendarConsentNotice(new URLSearchParams(window.location.search).get('calendar')))
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
  const [calendarSyncMode, setCalendarSyncMode] = useState('off')
  const [calendarBusy, setCalendarBusy] = useState(false)
  const calendarLifecycle = useRef(0)
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
  const resolution = useRequestResolution(apiURL, activeOrganizationID, `${activeUserID}:${requesterUserID}`)
  const accountName = authUser ? authUser.displayName.trim() || authUser.email.trim() || 'アカウント' : '山田 太郎'
  const policyDraftError = sharingPolicyError(sharingPolicy)

  useEffect(() => {
    const initialSearch = window.location.search
    const initialPath = window.location.pathname
    const params = new URLSearchParams(initialSearch)
    const authCompleted = params.get('auth') === 'success'
    const calendarCompleted = params.get('calendar') === 'connected'
    const calendarFailure = calendarConsentNotice(params.get('calendar'))
    const incomingInvitation = params.get('invite') ?? ''
    if (!authCompleted && !calendarCompleted && !calendarFailure && !incomingInvitation && import.meta.env.DEV) return
    let cancelled = false
    const lifecycle = calendarLifecycle.current
    const isCurrent = () => !cancelled && lifecycle === calendarLifecycle.current
    const loadSession = async () => {
      try {
        const response = await apiFetch(`${apiURL}/api/v1/auth/session`)
        if (!response.ok) throw new Error('session failed')
        const payload = await response.json() as { authenticated: boolean; demoMode?: boolean; user?: AuthUser }
        if (!isCurrent()) return
        setDemoMode(payload.demoMode === true)
        if (payload.authenticated && payload.user) {
          setAuthUser(payload.user)
          setProjections([])
          setMemberProjections([])
          setMemberPreview(false)
          if (authCompleted) setNotice('Googleアカウントでログインしました。')
          if (calendarCompleted) setNotice('Google Calendarを接続しました。同期を開始できます。')
          const calendarResponse = await apiFetch(`${apiURL}/api/v1/calendar/connection`)
          if (!isCurrent()) return
          if (calendarResponse.ok) {
            const calendarPayload = await calendarResponse.json() as { connected: boolean; connection?: CalendarConnection; syncMode?: string }
            if (!isCurrent()) return
            setCalendarConnection(calendarPayload.connected ? calendarPayload.connection ?? null : null)
            setCalendarSyncMode(calendarPayload.syncMode ?? 'off')
          }
          const workspaceResponse = await apiFetch(`${apiURL}/api/v1/workspaces`)
          if (!isCurrent()) return
          if (workspaceResponse.ok) {
            const workspacePayload = await workspaceResponse.json() as { workspaces: Workspace[] }
            if (!isCurrent()) return
            setWorkspaces(workspacePayload.workspaces)
          }
          if (incomingInvitation) {
            setInvitationToken(incomingInvitation)
            const invitationResponse = await apiFetch(`${apiURL}/api/v1/invitations/preview`, {
              method: 'POST', headers: { 'Content-Type': 'application/json' },
              body: JSON.stringify({ token: incomingInvitation }),
            })
            if (!isCurrent()) return
            if (invitationResponse.ok) {
              const preview = await invitationResponse.json() as InvitationPreview
              if (isCurrent()) setInvitationPreview(preview)
            }
            else setNotice('招待リンクは無効、期限切れ、または使用済みです。')
          }
        }
      } catch {
        if (isCurrent() && (authCompleted || calendarCompleted || calendarFailure)) setNotice('ログイン状態を確認できませんでした。もう一度ログインして接続状態を確認してください。')
      } finally {
        if (isCurrent() && window.location.pathname === initialPath && window.location.search === initialSearch && (authCompleted || calendarCompleted || calendarFailure || incomingInvitation)) window.history.replaceState({}, '', initialPath)
      }
    }
    void loadSession()
    return () => { cancelled = true }
  }, [])


  useEffect(() => {
    if (!authUser || !calendarConnection || currentView !== 'calendar') return
    let cancelled = false
    const lifecycle = calendarLifecycle.current
    const isCurrent = () => !cancelled && lifecycle === calendarLifecycle.current
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
        if (isCurrent()) setPrivateCalendarEvents(privatePayload.events ?? [])
        if (projectionResponse.ok) {
          const projectionPayload = await projectionResponse.json() as PublicProjection
          if (isCurrent()) setProjections(mapPublicSegments(projectionPayload))
        }
      } catch (error) {
        if (isCurrent()) {
          setPrivateCalendarEvents([])
          setPrivateEventsError(error instanceof Error && error.message === 'reconnect'
            ? 'Google Calendarの再接続が必要です。'
            : '本人用カレンダーを取得できませんでした。')
        }
      } finally {
        if (isCurrent()) setPrivateEventsLoading(false)
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
    ? calendarConnection ? privateCalendarEvents.map((event) => privateEventRow(event, calendarView)) : []
    : demoMode ? demoPrivateEvents : []


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
    try {
      const result = await resolution.resolve(requestID, requesterUserID, 'cancel')
      if (!result) return
      setSentRequests((current) => current.map((item) => item.id === requestID
        ? { ...item, status: 'cancelled' }
        : item))
      setNotice('依頼をキャンセルしました。相手にも通知しました。')
    } catch (error) {
      setNotice(error instanceof Error ? error.message : '一覧を更新して状態を確認してください。')
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
    if (action === 'decline') {
      try {
        const result = await resolution.resolve(requestID, activeUserID, 'decline')
        if (!result) return
        setInboxRequests(current => current.map(item => item.id === requestID ? { ...item, status: result.status } : item))
        setNotice('依頼を辞退しました。')
      } catch (error) { setNotice(error instanceof Error ? error.message : '一覧を更新して状態を確認してください。') }
      return
    }
    setRespondingRequestID(requestID)
    try {
      const response = await apiFetch(`${apiURL}/api/v1/requests/${requestID}/${action}`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'X-Demo-User-ID': activeUserID,
          'X-Organization-ID': activeOrganizationID,
        },
        body: JSON.stringify({ optionId: optionID }),
      })
      if (!response.ok) {
        if (response.status === 409) {
          const conflict = await response.json() as { code?: string }
          const messages: Record<string, string> = {
            candidate_expired: 'この候補は開始済み、または依頼の期限外です。新しい日時で依頼・提案してください。',
            candidate_invalid: 'この候補は会議として確定できません。別の時間を提案してください。',
            availability_changed: '公開された対応可能時間が変わったか、同期を確認できません。同期・更新後に別の時間を提案してください。',
            booking_conflict: '重なる確定済みの調整、または同時更新を検出しました。更新して確認し、必要なら別の時間を提案してください。',
          }
          setNotice(messages[conflict.code ?? ''] ?? '依頼の状態が変わりました。更新して確認してください。')
          return
        }
        throw new Error('response failed')
      }
      setInboxRequests((current) => current.map((item) => item.id === requestID
        ? { ...item, status: 'accepted', acceptedOptionId: optionID }
        : item))
      setNotice('候補を承認しました。')
    } catch {
      setNotice('依頼を更新できませんでした。最新状態を確認してください。')
    } finally {
      setRespondingRequestID('')
    }
  }

  const rescheduleMeeting = async (requestID: string, command: RescheduleCommand) => {
    const response = await apiFetch(`${apiURL}/api/v1/requests/${encodeURIComponent(requestID)}/reschedule`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'X-Demo-User-ID': currentView === 'sent' ? requesterUserID : activeUserID },
      body: JSON.stringify(command),
    })
    if (!response.ok) throw new Error('reschedule failed')
    const value = await response.json() as CoordinationRequest
    const update = (current: CoordinationRequest[]) => current.map((item) => item.id === requestID ? value : item)
    setInboxRequests(update)
    setSentRequests(update)
    setNotice(command.action === 'accept' ? '日時変更を確定しました。外部カレンダーの予定は手動で更新してください。' : '日時変更の交渉を更新しました。元の予約は維持されています。')
  }

  const cancelConfirmedMeeting = async (requestID: string, optionID: string) => {
    const response = await apiFetch(`${apiURL}/api/v1/requests/${encodeURIComponent(requestID)}/cancel-confirmed`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'X-Demo-User-ID': currentView === 'sent' ? requesterUserID : activeUserID },
      body: JSON.stringify({ optionId: optionID }),
    })
    if (!response.ok) throw new Error('confirmed cancellation failed')
    const update = (current: CoordinationRequest[]) => current.map((item) => item.id === requestID ? { ...item, status: 'cancelled' } : item)
    setInboxRequests(update)
    setSentRequests(update)
    setNotice('確定会議を取り消し、相手に通知しました。外部カレンダーの予定は手動で削除してください。')
  }

  const downloadConfirmedMeeting = async (requestID: string) => {
    const response = await apiFetch(`${apiURL}/api/v1/requests/${encodeURIComponent(requestID)}/calendar.ics`, {
      headers: { 'X-Demo-User-ID': currentView === 'sent' ? requesterUserID : activeUserID },
    })
    if (!response.ok) throw new Error('calendar export failed')
    const url = URL.createObjectURL(await response.blob())
    const link = document.createElement('a')
    link.href = url
    link.download = 'negotiable-meeting.ics'
    document.body.appendChild(link)
    link.click()
    link.remove()
    window.setTimeout(() => URL.revokeObjectURL(url), 1000)
  }

  const respondAsync = async (event: FormEvent<HTMLFormElement>, requestID: string) => {
    event.preventDefault()
    const formElement = event.currentTarget
    const message = String(new FormData(formElement).get('asyncMessage')).trim()
    try {
      const payload = await resolution.resolve(requestID, activeUserID, 'async', message)
      if (!payload) return
      setInboxRequests((current) => current.map((item) => item.id === requestID
        ? { ...item, status: payload.status, asyncMessage: payload.asyncMessage }
        : item))
      setNotice('非同期で回答しました。依頼者に通知しました。')
      formElement.reset()
    } catch (error) {
      setNotice(error instanceof Error ? error.message : '一覧を更新して状態を確認してください。')
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
      // Fence pending bootstrap/private reads/manual sync before React cleanup.
      calendarLifecycle.current++
      setAuthUser(null)
      setCalendarConnection(null)
      setCalendarBusy(false)
      setCalendarSyncMode('off')
      setWorkspaces([])
      setInvitationPreview(null)
      setInvitationToken('')
      setPrivateCalendarEvents([])
      setPrivateEventsLoading(false)
      setPrivateEventsError('')
      setSelectedPrivateEventID('')
      setProjections([])
      setMemberProjections([])
      setActiveDialog('')
      setAccountOpen(false)
      window.history.replaceState({}, '', window.location.pathname)
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
    if (policyDraftError) {
      setNotice(policyDraftError)
      return
    }
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
      calendarLifecycle.current++
      setPrivateCalendarEvents([])
      setPrivateEventsLoading(false)
      setPrivateEventsError('')
      setProjections([])
      setMemberProjections([])
      setSelectedPrivateEventID('')
      setAuthUser(null)
      setCalendarConnection(null)
      setCalendarBusy(false)
      setCalendarSyncMode('off')
      setWorkspaces([])
      setInvitationPreview(null)
      setInvitationToken('')
      setAccountOpen(false)
      window.history.replaceState({}, '', window.location.pathname)
      setNotice('ログアウトしました。')
    } catch {
      setNotice('ログアウトできませんでした。')
    }
  }

  const syncCalendar = async () => {
    const lifecycle = calendarLifecycle.current
    const isCurrent = () => lifecycle === calendarLifecycle.current
    setCalendarBusy(true)
    try {
      const response = await apiFetch(`${apiURL}/api/v1/calendar/sync`, { method: 'POST' })
      if (!response.ok) throw new Error('sync failed')
      const payload = await response.json() as { busySpanCount: number; lastSyncedAt: string }
      if (!isCurrent()) return
      setCalendarConnection((current) => current ? { ...current, lastSyncedAt: payload.lastSyncedAt, reconnectRequired: false, lastErrorCode: '', sourceFresh: true } : current)
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
          if (isCurrent()) setProjections(mapPublicSegments(view))
        }
      } catch {
        if (isCurrent()) setNotice(`Google Calendarから${payload.busySpanCount}件を同期しましたが、表示の再読込に失敗しました。`)
      }
    } catch {
      if (!isCurrent()) return
      setNotice('Calendarを同期できませんでした。再接続が必要な場合があります。')
      setCalendarConnection((current) => current ? { ...current, sourceFresh: false } : current)
    } finally {
      if (isCurrent()) setCalendarBusy(false)
    }
  }

  const disconnectCalendar = async () => {
    setCalendarBusy(true)
    try {
      const response = await apiFetch(`${apiURL}/api/v1/calendar/connection`, { method: 'DELETE' })
      if (!response.ok) throw new Error('disconnect failed')
      // Invalidate pending responses now, before React runs effect cleanup.
      calendarLifecycle.current++
      setCalendarConnection(null)
      setPrivateCalendarEvents([])
      setSelectedPrivateEventID('')
      setPrivateEventsError('')
      setPrivateEventsLoading(false)
      setProjections([])
      setMemberProjections([])
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
      <div className="app-shell signin-shell">
        <main className="signin-gate">
          <span className="brand-mark" aria-hidden="true">N</span>
          <p className="signin-brand">Negotiable Calendar</p>
          <h1>カレンダーにログイン</h1>
          <p className="hero-copy">自分の予定と組織に共有する公開状態を確認し、相談の依頼・回答を管理します。</p>
          <p className="signin-help">Google ログインとカレンダーの接続は別の操作です。予定の読み取りには、ログイン後に接続の許可が必要です。</p>
          {notice ? <p role="status">{notice}</p> : null}
          <a className="primary-button" href={`${apiURL}/api/v1/auth/google/login`}>Googleでログイン</a>
          <PolicyLinks />
          <CalendarConnectionHelp />
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
            <button className="avatar" type="button" aria-label={`${accountName}のアカウントメニュー`} aria-expanded={accountOpen} onClick={() => setAccountOpen(!accountOpen)}>
              <AccountAvatar name={accountName} imageUrl={authUser?.avatarUrl} />
            </button>
            {accountOpen ? (
              <div className="account-menu">
                <strong>{accountName}</strong>
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
                        <CalendarSyncStatus mode={calendarSyncMode} connection={calendarConnection} />
                        {calendarConnection.reconnectRequired ? (
                          <CalendarConsent connectURL={`${apiURL}/api/v1/calendar/google/connect`} reconnect />
                        ) : (
                          <button type="button" onClick={syncCalendar} disabled={calendarBusy}>{calendarBusy ? '処理中…' : 'busy時間を同期'}</button>
                        )}
                        <button type="button" onClick={disconnectCalendar} disabled={calendarBusy}>Calendar接続を解除</button>
                      </>
                    ) : (
                      <CalendarConsent connectURL={`${apiURL}/api/v1/calendar/google/connect`} />
                    )}
                    <button type="button" onClick={logout}>ログアウト</button>
                    <button className="danger-link" type="button" onClick={() => { setAccountOpen(false); setActiveDialog('delete-account') }}>アカウントを削除</button>
                  </>
                ) : (
                  <a href={`${apiURL}/api/v1/auth/google/login`}>Googleでログイン</a>
                )}
                <button type="button" onClick={exportUserData} disabled={exporting}>{exporting ? '準備中…' : '本人データをエクスポート'}</button>
                {(!authUser || (calendarConnection && !calendarConnection.reconnectRequired)) ? <PolicyLinks /> : null}
              </div>
            ) : null}
          </div>
        </div>
      </header>

      <aside className="workspace-nav">
        <nav className="top-nav" aria-label="メインナビゲーション">
          <button aria-current={currentView === 'calendar' ? 'page' : undefined} className={currentView === 'calendar' ? 'active' : ''} type="button" onClick={() => setCurrentView('calendar')}><NavigationIcon view="calendar" />マイカレンダー</button>
          <button aria-current={currentView === 'people' ? 'page' : undefined} className={currentView === 'people' ? 'active' : ''} type="button" onClick={openPeopleView}><NavigationIcon view="people" />組織</button>
          <button aria-current={currentView === 'inbox' ? 'page' : undefined} className={currentView === 'inbox' ? 'active' : ''} type="button" onClick={openInbox}><NavigationIcon view="inbox" />依頼</button>
          <button aria-current={currentView === 'sent' ? 'page' : undefined} className={currentView === 'sent' ? 'active' : ''} type="button" onClick={openSentRequests}><NavigationIcon view="sent" />送信済み</button>
          <button aria-current={currentView === 'audit' ? 'page' : undefined} className={currentView === 'audit' ? 'active' : ''} type="button" onClick={openAudit}><NavigationIcon view="audit" />監査</button>
        </nav>
        <div className="workspace-note"><ShieldIcon /><p>組織への共有範囲<strong>公開状態のみ</strong></p></div>
      </aside>

      <main id="top">
        {demoMode ? <p className="demo-notice">デモ表示 · サンプルの予定です。実際のカレンダーとは同期していません。</p> : null}
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
            <h1 id="page-title">カレンダー</h1>
            <p className="hero-copy">自分の予定と、組織に共有する相談可能な時間を確認できます。</p>
          </div>
          <button className="primary-button" type="button" onClick={() => { setRequestTargetID(''); setActiveDialog('request') }}>
            <span aria-hidden="true">＋</span> 依頼を作成
          </button>
        </section>

        <section className="privacy-note" aria-label="プライバシー設定の状態">
          <ShieldIcon />
          <div>
            <strong>公開範囲：状態のみ</strong>
            <span>イベント名・参加者・場所は組織に共有されません</span>
          </div>
          <button type="button" onClick={openSharingRules}>共有ルールを確認</button>
        </section>

        <section className="calendar-toolbar" aria-label="カレンダー操作">
          <strong className="calendar-date">{dateLabel}</strong>
          <div className="date-controls">
            <button type="button" aria-label={calendarView === 'day' ? '前の日' : calendarView === 'week' ? '前の週' : '前の月'} onClick={() => moveCalendar(-1)}>←</button>
            <button type="button" onClick={() => setCalendarAnchor(new Date())}>今日</button>
            <button type="button" aria-label={calendarView === 'day' ? '次の日' : calendarView === 'week' ? '次の週' : '次の月'} onClick={() => moveCalendar(1)}>→</button>
          </div>
          <div className="calendar-view-controls" aria-label="表示期間">
            {(['day', 'week', 'month'] as CalendarView[]).map((view) => (
              <button aria-pressed={calendarView === view} className={calendarView === view ? 'active' : ''} type="button" key={view} onClick={() => setCalendarView(view)}>
                {view === 'day' ? '日' : view === 'week' ? '週' : '月'}
              </button>
            ))}
          </div>
          <div className="layer-controls" aria-label="表示レイヤー">
            <button aria-pressed={calendarLayer === 'both'} className={calendarLayer === 'both' ? 'active' : ''} type="button" onClick={() => setCalendarLayer('both')}>両方</button>
            <button aria-pressed={calendarLayer === 'private'} className={calendarLayer === 'private' ? 'active' : ''} type="button" onClick={() => setCalendarLayer('private')}>自分の予定</button>
            <button aria-pressed={calendarLayer === 'projection'} className={calendarLayer === 'projection' ? 'active' : ''} type="button" onClick={() => setCalendarLayer('projection')}>公開状態</button>
          </div>
          <button className="override-button" type="button" onClick={() => setActiveDialog('override')}>状態を上書き</button>
        </section>

        <section className={memberPreview ? 'calendar-grid member-preview' : `calendar-grid layer-${calendarLayer}`} aria-label="今日のプライベート予定と公開状態">
          <article className="calendar-panel private-panel">
            <div className="panel-heading">
              <div>
                <h2>あなたの予定</h2>
                <p>予定を選択すると詳細を表示</p>
              </div>
              <span className="private-label">自分のみ</span>
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
                    <span>{selectedPrivateEventID === event.id ? '詳細を閉じる' : '詳細を確認'}</span>
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
                <h2>組織に見える状態</h2>
                <p>共有ルールから生成した相談可能性</p>
              </div>
              <span className="projection-label"><span /> 組織に公開</span>
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
              {!memberPreview && authUser && displayedProjections.length === 0 ? (
                <p className="empty-state">表示できる公開状態はありません。共有ルールとカレンダー接続・同期状態を確認してください。</p>
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
                <h1 id="people-title">組織の公開状態</h1>
                <p className="hero-copy">メンバーが共有する相談可能な時間を確認し、依頼を作成できます。</p>
                <p className="field-help">外部カレンダー未接続の表示は共有方針に基づきます。デモは実予定の同期結果ではありません。</p>
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
                <article className="person-row" key={person.id}>
                  <div className="person-profile">
                    <span className="person-avatar">{person.displayName.slice(0, 1)}</span>
                    <div>
                      <h2>{person.displayName}</h2>
                      <p>{person.role} · {person.timezone}</p>
                    </div>
                    <button type="button" disabled={person.id === requesterUserID} onClick={() => { setRequestTargetID(person.id); setActiveDialog('request') }}>依頼を作成</button>
                  </div>
                  <div className="person-timeline" aria-label={`${person.displayName}の公開状態`}>
                    {person.segments.map((segment) => (
                      <div className={`person-segment ${segment.tone}`} key={`${person.id}-${segment.time}`}>
                        <time>{segment.time}</time>
                        <strong>{segment.label}</strong>
                      </div>
                    ))}
                    {person.segments.length === 0 ? <p>確認できる公開状態はありません。同期の鮮度や共有条件を確認してください。</p> : null}
                  </div>
                </article>
              ))}
            </div>
          </section>
        ) : currentView === 'inbox' ? (
          <section className="inbox-view" aria-labelledby="inbox-title">
            <div className="people-heading">
              <div>
                <h1 id="inbox-title">受信した依頼</h1>
                <p className="hero-copy">日時の承認・別時間の提案・非同期での回答・委譲を行えます。</p>
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
                <article className="request-row" key={item.id}>
                  <div className="request-summary">
                    <div className="request-meta">
                      <span className={`priority priority-${item.priority}`}>{priorityLabel(item.priority)}</span>
                      <span>{requestStatusLabel(item.status)}</span>
                    </div>
                    <h2>{item.title}</h2>
                    <p>{item.requesterUserId} · {item.durationMinutes}分 · 期限 {formatDateTime(item.deadlineAt)}</p>
                  </div>
                  <div className="option-list" aria-label={`${item.title}の候補`}>
                    {authUser && item.targetUserId === activeUserID && item.status === 'suggested' ? <LocalPlanning
                      key={JSON.stringify([activeUserID, activeOrganizationID, item.id, item.deadlineAt, item.options])}
                      loadPreview={async (signal) => {
                        const response = await apiFetch(`${apiURL}/api/v1/requests/${encodeURIComponent(item.id)}/planning-preview`, {
                          signal, headers: { 'X-Demo-User-ID': activeUserID, 'X-Organization-ID': activeOrganizationID },
                        })
                        if (response.status === 429) throw new PlanningRateLimitError(response.headers.get('Retry-After'))
                        if (!response.ok) throw new Error('planning preview unavailable')
                        return response.json()
                      }}
                    /> : null}
                    <ConfirmedMeeting request={item} onDownload={downloadConfirmedMeeting} onCancel={cancelConfirmedMeeting} />
                    <RescheduleMeeting request={item} actor={activeUserID} onChange={rescheduleMeeting} />
                    {item.options.map((option) => (
                      <div className="option-row" key={option.id}>
                        <span>{option.type === 'meeting' ? '会議' : '非同期'}</span>
                        <strong>{option.startAt && option.endAt
                          ? `${formatDateTime(option.startAt)} — ${new Intl.DateTimeFormat('ja-JP', { hour: '2-digit', minute: '2-digit', hour12: false }).format(new Date(option.endAt))}`
                          : '非同期で回答'}</strong>
                        {item.status === 'suggested' && option.type === 'meeting' && !option.proposedByUserId ? (
                          <button type="button" disabled={respondingRequestID === item.id || resolution.pending(item.id)} onClick={() => respondToRequest(item.id, 'accept', option.id)}>この候補を承認</button>
                        ) : item.status === 'suggested' && option.proposedByUserId && option.proposedByUserId === item.targetUserId ? <span>依頼者の承認待ち</span> : null}
                      </div>
                    ))}
                    {item.status === 'suggested' ? (
                      <>
                        <CounterproposalForm key={`proposal:${activeOrganizationID}:${activeUserID}:${item.id}`}
                          apiURL={apiURL} organizationID={activeOrganizationID} actor={activeUserID} requestID={item.id}
                          durationMinutes={item.durationMinutes} disabled={respondingRequestID === item.id || resolution.pending(item.id)}
                          onProposed={option => { setInboxRequests(current => current.map(row => row.id === item.id ? { ...row, options: [...row.options.filter(old => old.id !== option.id), option] } : row)); setNotice('別の時間を提案しました。依頼者の承認を待っています。') }} />
                        <form className="async-form" onSubmit={(event) => respondAsync(event, item.id)}>
                          <label>非同期メッセージ<textarea name="asyncMessage" maxLength={500} rows={2} placeholder="回答方法や次のアクションを500文字以内で入力" disabled={resolution.pending(item.id)} required /></label>
                          <button type="submit" disabled={respondingRequestID === item.id || resolution.pending(item.id)}>非同期で回答</button>
                        </form>
                        {!item.delegatedFromUserId ? <RequestHandoff
                          key={`${activeOrganizationID}:${activeUserID}:${item.id}`}
                          apiURL={apiURL} organizationID={activeOrganizationID} actor={activeUserID} requestID={item.id} requesterID={item.requesterUserId}
                          disabled={respondingRequestID === item.id || resolution.pending(item.id)}
                          onDone={name => { setInboxRequests(current => current.filter(request => request.id !== item.id)); setNotice(`${name} に担当を引き継ぎました。依頼者にも通知しました。`) }}
                        /> : <p className="field-help">引き継いだ依頼です。再委譲はできません。</p>}
                        <button className="decline-button" type="button" disabled={respondingRequestID === item.id || resolution.pending(item.id)} onClick={() => respondToRequest(item.id, 'decline')}>今回は辞退</button>
                      </>
                    ) : <div><p className="response-complete">回答済み · {requestStatusLabel(item.status)}</p>{item.asyncMessage ? <p className="async-message">{item.asyncMessage}</p> : null}</div>}
                  </div>
                </article>
              ))}
            </div>
          </section>
        ) : currentView === 'sent' ? (
          <section className="inbox-view" aria-labelledby="sent-title">
            <div className="people-heading">
              <div>
                <h1 id="sent-title">送信した依頼</h1>
                <p className="hero-copy">相手からの時間提案を承認し、確定日時を確認できます。回答前の依頼は取り消せます。</p>
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
                  <article className="request-row" key={item.id}>
                    <div className="request-summary">
                      <div className="request-meta">
                        <span className={`priority priority-${item.priority}`}>{priorityLabel(item.priority)}</span>
                        <span>{requestStatusLabel(item.status)}</span>
                      </div>
                      <h2>{item.title}</h2>
                      <p>{item.targetUserId} 宛 · {item.durationMinutes}分 · 期限 {formatDateTime(item.deadlineAt)}</p>
                    </div>
                    <div className="option-list">
                      <ConfirmedMeeting request={item} onDownload={downloadConfirmedMeeting} onCancel={cancelConfirmedMeeting} />
                      <RescheduleMeeting request={item} actor={requesterUserID} onChange={rescheduleMeeting} />
                      <strong>{item.options.length}件の調整候補</strong>
                      <CounterproposalAgreement key={`${activeOrganizationID}:${requesterUserID}:${item.id}`}
                        apiURL={apiURL} organizationID={activeOrganizationID} actor={requesterUserID} requestID={item.id}
                        targetID={item.targetUserId} status={item.status} options={item.options}
                        disabled={respondingRequestID === item.id || resolution.pending(item.id)}
                        onConfirmed={optionID => { setSentRequests(current => current.map(row => row.id === item.id ? { ...row, status: 'accepted', acceptedOptionId: optionID } : row)); setNotice('提案を承認し、会議を確定しました。相手に通知しました。') }} />
                      {item.delegatedFromUserId ? <p className="field-help">担当変更済み · 現在の担当: {item.targetUserId}</p> : null}
                      {cancellable ? (
                        <button className="decline-button" type="button" disabled={respondingRequestID === item.id || resolution.pending(item.id)} onClick={() => cancelSentRequest(item.id)}>
                          {respondingRequestID === item.id || resolution.pending(item.id) ? 'キャンセル中…' : '依頼をキャンセル'}
                        </button>
                      ) : <div><p className="response-complete">更新済み · {requestStatusLabel(item.status)}</p>{item.asyncMessage ? <p className="async-message">{item.asyncMessage}</p> : null}</div>}
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
                <h1 id="audit-title">操作履歴</h1>
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

      {activeDialog === 'request' ? <RequestComposer
        key={`${activeOrganizationID}:${requesterUserID}`}
        apiURL={apiURL} organizationID={activeOrganizationID} requesterID={requesterUserID}
        initialTargetID={requestTargetID} demo={demoMode && !authUser}
        onClose={() => setActiveDialog('')}
        onCreated={(count) => { setActiveDialog(''); setNotice(count > 0 ? `依頼を送信し、${count}件の候補を生成しました。` : '依頼を送信しました。') }}
      /> : null}

      {activeDialog === 'rules' ? (
        <div className="modal-backdrop" role="presentation">
          <section className="modal rules-modal" role="dialog" aria-modal="true" aria-labelledby="rules-title">
            <div className="modal-heading">
              <div>
                <h2 id="rules-title">共有ルール</h2>
              </div>
              <button className="close-button" type="button" aria-label="閉じる" onClick={() => setActiveDialog('')}>×</button>
            </div>
            <p className="modal-copy">勤務時間と条件を指定し、組織に表示する相談可否・割り込み可否を設定します。</p>
            {policyLoading ? <p role="status">共有ルールを読み込んでいます…</p> : null}
            <SharingPolicyEditor value={sharingPolicy} onChange={setSharingPolicy} disabled={policyLoading || policySaving} />
            <div className="policy-save-actions">
              <button className="secondary-button" type="button" disabled={policySaving} onClick={() => setActiveDialog('')}>キャンセル</button>
              <button className="primary-button" type="button" onClick={saveSharingRules} disabled={policyLoading || policySaving || Boolean(policyDraftError)}>{policySaving ? '保存中…' : '保存して公開状態を更新'}</button>
            </div>
          </section>
        </div>
      ) : null}

      {activeDialog === 'override' ? (
        <div className="modal-backdrop" role="presentation">
          <section className="modal" role="dialog" aria-modal="true" aria-labelledby="override-title">
            <div className="modal-heading">
              <div>
                <h2 id="override-title">公開状態を上書き</h2>
              </div>
              <button className="close-button" type="button" aria-label="閉じる" onClick={() => setActiveDialog('')}>×</button>
            </div>
            <p className="modal-copy">指定した時間の公開状態を一時的に変更します。元のカレンダーの予定は変更しません。</p>
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
        <span>Negotiable Calendar</span>
        <span>表示時刻は端末のタイムゾーンに基づきます</span>
      </footer>
    </div>
  )
}

export default App
