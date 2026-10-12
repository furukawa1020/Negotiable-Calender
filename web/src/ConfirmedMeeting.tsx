import { useState } from 'react'
import { useMeetingClock } from './useMeetingClock'

type Props = {
  request: { id: string; status: string; acceptedOptionId?: string; rescheduleProposal?: { status: string }; options: Array<{ id: string; type: string; startAt?: string; endAt?: string }> }
  onDownload: (id: string) => Promise<void>
  onCancel?: (id: string, optionID: string) => Promise<void>
  onComplete?: (id: string, optionID: string) => Promise<void>
}

export function ConfirmedMeeting({ request, onDownload, onCancel, onComplete }: Props) {
  const selected = request.options.filter((item) => item.id === request.acceptedOptionId)
  // A confirmation belongs to the exact evidence the participant saw. Remount
  // also isolates late local async state from a different meeting/version.
  const identity = JSON.stringify([request.id, request.status, selected, request.rescheduleProposal])
  return <MeetingActions key={identity} request={request} onDownload={onDownload} onCancel={onCancel} onComplete={onComplete} />
}

function MeetingActions({ request, onDownload, onCancel, onComplete }: Props) {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [confirming, setConfirming] = useState(false)
  const [completing, setCompleting] = useState(false)
  const selected = request.options.filter((item) => item.id === request.acceptedOptionId)
  const option = selected.length === 1 && selected[0].type === 'meeting' ? selected[0] : undefined
  const now = useMeetingClock(Date.parse(option?.startAt ?? ''), Date.parse(option?.endAt ?? ''), request.status === 'accepted' && !!(onCancel || onComplete))
  if (request.status !== 'accepted' && request.status !== 'completed') return null
  if (!option?.startAt || !option.endAt || !Number.isFinite(Date.parse(option.startAt)) || !Number.isFinite(Date.parse(option.endAt)) || Date.parse(option.endAt) <= Date.parse(option.startAt)) {
    return <p>確定日時を確認できません。依頼を更新してください。</p>
  }
  const format = new Intl.DateTimeFormat('ja-JP', { dateStyle: 'medium', timeStyle: 'short' })
  const download = async () => {
    setBusy(true)
    setError('')
    try { await onDownload(request.id) } catch { setError('取得できませんでした。依頼を更新して再試行してください。') } finally { setBusy(false) }
  }
  const cancel = async (commandTime: number) => {
    if (!onCancel) return
    if (Date.parse(option.startAt!) <= commandTime) {
      setConfirming(false)
      setError('開始時刻を過ぎたため取り消せません。依頼を更新してください。')
      return
    }
    setBusy(true)
    setError('')
    try {
      await onCancel(request.id, option.id)
      setConfirming(false)
    } catch {
      setError('取消を確認できませんでした。依頼を更新し、最新状態を確認して再試行してください。')
    } finally { setBusy(false) }
  }
  const complete = async (commandTime: number) => {
    if (!onComplete) return
    if (Date.parse(option.endAt!) > commandTime || request.rescheduleProposal?.status === 'proposed') {
      setCompleting(false)
      setError('終了日時・変更提案・最新状態を確認してください。')
      return
    }
    setBusy(true)
    setError('')
    try { await onComplete(request.id, option.id); setCompleting(false) }
    catch { setError('完了を確認できませんでした。終了日時・変更提案・最新状態を確認して再試行してください。') }
    finally { setBusy(false) }
  }
  return <section className="confirmed-meeting" aria-label="確定した会議">
    <strong>{request.status === 'completed' ? '完了した会議の日時' : '確定日時'}</strong>
    <p><time dateTime={option.startAt}>{format.format(new Date(option.startAt))}</time> — <time dateTime={option.endAt}>{format.format(new Date(option.endAt))}</time></p>
    <button type="button" disabled={busy} onClick={download}>{busy ? '取得中…' : 'カレンダーに登録（ICS）'}</button>
    <p>日時は端末のタイムゾーンで表示しています。ダウンロードしたICSをお使いのカレンダーに取り込んでください。Googleへの自動登録・招待送信は行いません。</p>
    {request.status === 'accepted' && onCancel && Date.parse(option.startAt) > now ? confirming ? <div>
      <p>この確定会議を取り消しますか？ 相手に通知し、予約枠を解放します。Googleなど外部カレンダーに取り込み済みの予定は自動削除されません。</p>
      <button type="button" disabled={busy} onClick={() => cancel(Date.now())}>会議の取消を確定する</button>
      <button type="button" disabled={busy} onClick={() => setConfirming(false)}>戻る</button>
    </div> : <button type="button" disabled={busy} onClick={() => setConfirming(true)}>確定会議を取り消す</button> : null}
    {request.status === 'accepted' && onComplete && Date.parse(option.endAt) <= now && request.rescheduleProposal?.status !== 'proposed' ? completing ? <div>
      <p>この会議を完了として記録しますか？ 相手に通知し、確定日時は履歴に残します。外部カレンダーの予定は変更しません。</p>
      <button type="button" disabled={busy} onClick={() => complete(Date.now())}>会議の完了を確定する</button>
      <button type="button" disabled={busy} onClick={() => setCompleting(false)}>戻る</button>
    </div> : <button type="button" disabled={busy} onClick={() => setCompleting(true)}>会議を完了にする</button> : null}
    {error ? <p role="alert">{error}</p> : null}
  </section>
}
