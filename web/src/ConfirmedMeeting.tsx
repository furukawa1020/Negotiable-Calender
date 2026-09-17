import { useState } from 'react'

type Props = {
  request: { id: string; status: string; acceptedOptionId?: string; options: Array<{ id: string; type: string; startAt?: string; endAt?: string }> }
  onDownload: (id: string) => Promise<void>
}

export function ConfirmedMeeting({ request, onDownload }: Props) {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
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
  return <section className="confirmed-meeting" aria-label="確定した会議">
    <strong>確定日時</strong>
    <p><time dateTime={option.startAt}>{format.format(new Date(option.startAt))}</time> — <time dateTime={option.endAt}>{format.format(new Date(option.endAt))}</time></p>
    <button type="button" disabled={busy} onClick={download}>{busy ? '取得中…' : 'カレンダーに登録（ICS）'}</button>
    <p>日時は端末のタイムゾーンで表示しています。ダウンロードしたICSをお使いのカレンダーに取り込んでください。Googleへの自動登録・招待送信は行いません。</p>
    {error ? <p role="alert">{error}</p> : null}
  </section>
}
