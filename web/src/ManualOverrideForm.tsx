import { useEffect, useRef, useState, type FormEvent } from 'react'
import { fetchBookingResult } from './bookingTransport'
import { overrideCommand } from './manualOverride'

type Props = { apiURL: string; userID: string; date: Date; isCurrent: () => boolean; onClose: () => void; onSaved: (start: string, end: string, availability: string) => void }
class OverrideRejected extends Error {}
export function ManualOverrideForm({ apiURL, userID, date, isCurrent, onClose, onSaved }: Props) {
  const [saving, setSaving] = useState(false), [uncertain, setUncertain] = useState(false), [error, setError] = useState('')
  const active = useRef<AbortController | null>(null)
  useEffect(() => () => active.current?.abort(), [])
  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (saving || uncertain || !isCurrent()) return
    const form = new FormData(event.currentTarget)
    const start = String(form.get('startTime')), end = String(form.get('endTime')), availability = String(form.get('availability'))
    let command: ReturnType<typeof overrideCommand>
    try { command = overrideCommand(date, start, end, availability) }
    catch { setError('開始・終了時刻と公開状態を確認してください。終了は開始より後にしてください。'); return }
    const controller = new AbortController(); active.current = controller
    const current = () => !controller.signal.aborted && isCurrent()
    setSaving(true); setError('')
    try {
      const saved = await fetchBookingResult(`${apiURL}/api/v1/users/${encodeURIComponent(userID)}/manual-overrides`, {
        method: 'POST', headers: { 'Content-Type': 'application/json', 'X-Demo-User-ID': userID }, body: JSON.stringify(command),
      }, async response => {
        const value = await response.json()
        if (!value || typeof value.id !== 'string' || !value.id.trim() || value.userId !== userID
          || !['startAt', 'endAt', 'expiresAt'].every(key => typeof value[key] === 'string' && Date.parse(value[key]) === Date.parse(command[key as 'startAt']))
          || !Object.entries(command.state).every(([key, expected]) => value.state?.[key] === expected)) throw new Error('unconfirmed override')
        return true
      }, current, { signal: controller.signal, readError: async response => { throw [400, 401, 403, 404, 422].includes(response.status) ? new OverrideRejected() : new Error('unknown override outcome') } })
      if (saved && current()) onSaved(start, end, availability)
    } catch (cause) {
      if (current()) {
        setUncertain(!(cause instanceof OverrideRejected))
        setError(cause instanceof OverrideRejected ? '上書きが拒否されました。入力内容と権限を確認してください。' : '保存結果を確認できませんでした。保存された可能性があるため、ページを再読み込みして公開状態を確認してください。')
      }
    } finally { if (current()) setSaving(false) }
  }
  return <form className="request-form" onSubmit={submit}>
    <div className="form-row"><label>開始<input name="startTime" type="time" defaultValue="15:30" disabled={saving || uncertain} required /></label><label>終了<input name="endTime" type="time" defaultValue="16:00" disabled={saving || uncertain} required /></label></div>
    <label>公開状態<select name="availability" defaultValue="available" disabled={saving || uncertain}><option value="available">相談可能</option><option value="limited">緊急のみ</option></select></label>
    {error ? <p role="alert">{error}</p> : null}
    {uncertain ? <button type="button" onClick={() => window.location.reload()}>ページを再読み込みして確認</button> : null}
    <div className="modal-actions"><button className="secondary-button" type="button" onClick={onClose}>キャンセル</button><button className="primary-button" type="submit" disabled={saving || uncertain}>{saving ? '保存中…' : '上書きを保存'}</button></div>
  </form>
}
