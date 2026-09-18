import { useState } from 'react'

type Props = {
  request: { id: string; status: string; acceptedOptionId?: string; options: Array<{ id: string; type: string; startAt?: string; endAt?: string }> }
  onDownload: (id: string) => Promise<void>
  onCancel?: (id: string, optionID: string) => Promise<void>
}

export function ConfirmedMeeting({ request, onDownload, onCancel }: Props) {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [confirming, setConfirming] = useState(false)
  const [openedAt] = useState(() => Date.now())
  if (request.status !== 'accepted') return null
  const option = request.options.find((item) => item.id === request.acceptedOptionId && item.type === 'meeting')
  if (!option?.startAt || !option.endAt || !Number.isFinite(Date.parse(option.startAt)) || !Number.isFinite(Date.parse(option.endAt)) || Date.parse(option.endAt) <= Date.parse(option.startAt)) {
    return <p>確定日時を確認できません。依頼を更新してください。</p>
  }
  const format = new Intl.DateTimeFormat('ja-JP', { dateStyle: 'medium', timeStyle: 'short' })
  const download = async () => {
    setBusy(true)
    setError('')
    try { await onDownload(request.id) } catch { setError('取得できませんでした。依頼を更新して再試行してください。') } finally { setBusy(false) }
  }
  const cancel = async () => {
    if (!onCancel) return
    setBusy(true)
    setError('')
    try {
      await onCancel(request.id, option.id)
      setConfirming(false)
    } catch {
      setError('取消を確認できませんでした。依頼を更新し、最新状態を確認して再試行してください。')
    } finally { setBusy(false) }
  }
  return <section className="confirmed-meeting" aria-label="確定した会議">
    <strong>確定日時</strong>
    <p><time dateTime={option.startAt}>{format.format(new Date(option.startAt))}</time> — <time dateTime={option.endAt}>{format.format(new Date(option.endAt))}</time></p>
    <button type="button" disabled={busy} onClick={download}>{busy ? '取得中…' : 'カレンダーに登録（ICS）'}</button>
    <p>日時は端末のタイムゾーンで表示しています。ダウンロードしたICSをお使いのカレンダーに取り込んでください。Googleへの自動登録・招待送信は行いません。</p>
    {onCancel && Date.parse(option.startAt) > openedAt ? confirming ? <div>
      <p>この確定会議を取り消しますか？ 相手に通知し、予約枠を解放します。Googleなど外部カレンダーに取り込み済みの予定は自動削除されません。</p>
      <button type="button" disabled={busy} onClick={cancel}>会議の取消を確定する</button>
      <button type="button" disabled={busy} onClick={() => setConfirming(false)}>戻る</button>
    </div> : <button type="button" disabled={busy} onClick={() => setConfirming(true)}>確定会議を取り消す</button> : null}
    {error ? <p role="alert">{error}</p> : null}
  </section>
}
