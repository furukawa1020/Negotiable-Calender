import { useRef, useState } from 'react'
import type { FormEvent } from 'react'
import { useMeetingClock } from './useMeetingClock'

export type RescheduleProposal = { id: string; proposerUserId: string; expectedOptionId: string; status: string }
export type RescheduleCommand = { action: 'propose' | 'accept' | 'decline' | 'withdraw'; proposalId: string; expectedOptionId: string; startAt?: string }
type Props = {
  request: { id: string; status: string; acceptedOptionId?: string; durationMinutes: number; deadlineAt: string; rescheduleProposal?: RescheduleProposal; options: Array<{ id: string; type: string; startAt?: string; endAt?: string }> }
  actor: string
  onChange: (id: string, command: RescheduleCommand) => Promise<void>
}

export function RescheduleMeeting({ request, actor, onChange }: Props) {
  return <RescheduleActions key={JSON.stringify([actor, request])} request={request} actor={actor} onChange={onChange} />
}

function RescheduleActions({ request, actor, onChange }: Props) {
  const [editing, setEditing] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const retry = useRef<{ start: string; expected: string; id: string } | null>(null)
  const selectedOptions = request.options.filter(option => option.id === request.acceptedOptionId)
  const selected = selectedOptions.length === 1 && selectedOptions[0].type === 'meeting' ? selectedOptions[0] : undefined
  const proposal = request.rescheduleProposal
  const pending = proposal?.status === 'proposed'
  const proposedOptions = request.options.filter(option => option.id === proposal?.id)
  const proposed = proposedOptions.length === 1 && proposedOptions[0].type === 'meeting' ? proposedOptions[0] : undefined
  const selectedStart = Date.parse(selected?.startAt ?? ''), selectedEnd = Date.parse(selected?.endAt ?? '')
  const proposedStart = Date.parse(proposed?.startAt ?? ''), proposedEnd = Date.parse(proposed?.endAt ?? '')
  const boundary = pending && Number.isFinite(proposedStart) ? proposedStart : selectedStart
  const now = useMeetingClock(Math.min(selectedStart, boundary), Math.max(selectedStart, boundary), request.status === 'accepted')
  if (request.status !== 'accepted' || !actor || !selected || !Number.isFinite(selectedStart) || !Number.isFinite(selectedEnd) || selectedEnd <= selectedStart || selectedStart <= now) return null
  const durationMinutes = (selectedEnd - selectedStart) / 60000
  if (pending && (!proposal.id || !proposal.proposerUserId || proposal.expectedOptionId !== selected.id || proposal.id === selected.id || !proposed || !Number.isFinite(proposedStart) || !Number.isFinite(proposedEnd) || proposedEnd - proposedStart !== selectedEnd - selectedStart)) {
    return <p role="alert">変更提案の日時を確認できません。依頼を更新してください。</p>
  }
  const run = async (command: RescheduleCommand) => {
    setBusy(true)
    setError('')
    try {
      await onChange(request.id, command)
      setEditing(false)
      retry.current = null
    } catch {
      setError('日時変更の結果を確認できませんでした。依頼を更新して確定日時・競合・期限・相手の応答を確認し、再試行してください。')
    } finally { setBusy(false) }
  }
  const propose = (event: FormEvent<HTMLFormElement>, commandTime: number) => {
    event.preventDefault()
    if (selectedStart <= commandTime) { setError('開始時刻を過ぎたため変更できません。依頼を更新してください。'); return }
    const start = new Date(String(new FormData(event.currentTarget).get('startAt')))
    if (!Number.isFinite(start.getTime())) { setError('有効な日時を入力してください。'); return }
    const iso = start.toISOString()
    if (!retry.current || retry.current.start !== iso || retry.current.expected !== selected.id) {
      retry.current = { start: iso, expected: selected.id, id: crypto.randomUUID() }
    }
    void run({ action: 'propose', proposalId: retry.current.id, expectedOptionId: selected.id, startAt: iso })
  }
  const respond = (action: 'accept' | 'decline' | 'withdraw', commandTime: number) => {
    if (selectedStart <= commandTime || (action === 'accept' && proposedStart <= commandTime)) { setError('開始時刻を過ぎたため変更できません。依頼を更新してください。'); return }
    if (proposal) void run({ action, proposalId: proposal.id, expectedOptionId: proposal.expectedOptionId })
  }
  return <section className="confirmed-meeting" aria-label="日時変更の交渉">
    <strong>日時変更</strong>
    <p>相手が承認するまでは元の予約を維持します。提案先の時間はまだ予約されません。</p>
    {pending ? <>
      <p>変更提案: {proposed?.startAt ? new Date(proposed.startAt).toLocaleString('ja-JP') : '日時を確認できません'}（{durationMinutes}分）</p>
      {proposal.proposerUserId === actor ? <button type="button" disabled={busy} onClick={() => respond('withdraw', Date.now())}>変更提案を撤回</button> : <>
        <button type="button" disabled={busy || proposedStart <= now} onClick={() => respond('accept', Date.now())}>この日時への変更を承認</button>
        <button type="button" disabled={busy} onClick={() => respond('decline', Date.now())}>変更提案を辞退</button>
      </>}
    </> : editing ? <form onSubmit={event => propose(event, Date.now())}>
      <label>変更後の開始日時<input name="startAt" type="datetime-local" required disabled={busy} /></label>
      <p>端末のタイムゾーンで入力してください。所要時間は{durationMinutes}分、終了は元の依頼期限（{new Date(request.deadlineAt).toLocaleString('ja-JP')}）までです。</p>
      <button type="submit" disabled={busy}>日時変更の提案を送信</button>
      <button type="button" disabled={busy} onClick={() => setEditing(false)}>閉じる</button>
    </form> : <button type="button" disabled={busy} onClick={() => setEditing(true)}>日時変更を提案</button>}
    <p>変更が確定しても、Google・ICS に登録済みの予定は自動更新されません。手動で更新してください。</p>
    {error ? <p role="alert">{error}</p> : null}
  </section>
}
