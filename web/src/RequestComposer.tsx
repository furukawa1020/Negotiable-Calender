import { useEffect, useRef, useState } from 'react'
import type { FormEvent } from 'react'

type Recipient = { id: string; displayName: string }
type Props = {
  apiURL: string
  organizationID: string
  requesterID: string
  initialTargetID: string
  demo: boolean
  onClose: () => void
  onCreated: (optionCount: number) => void
}

function initialDeadline() {
  const date = new Date()
  date.setHours(17, 0, 0, 0)
  if (date.getTime() <= Date.now()) date.setDate(date.getDate() + 1)
  const local = new Date(date.getTime() - date.getTimezoneOffset() * 60_000)
  return local.toISOString().slice(0, 16)
}

export function RequestComposer({ apiURL, organizationID, requesterID, initialTargetID, demo, onClose, onCreated }: Props) {
  const [recipients, setRecipients] = useState<Recipient[]>(demo ? [{ id: 'demo-manager', displayName: '山田 太郎（デモ）' }] : [])
  const [target, setTarget] = useState(demo ? 'demo-manager' : initialTargetID)
  const [loading, setLoading] = useState(!demo)
  const [directoryError, setDirectoryError] = useState('')
  const [generation, setGeneration] = useState(0)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const [deadline] = useState(initialDeadline)
  const attempt = useRef<{ payload: string; key: string } | null>(null)
  const sending = useRef(false)
  const alive = useRef(true)
  const submission = useRef<AbortController | null>(null)
  useEffect(() => {
    alive.current = true
    return () => { alive.current = false; submission.current?.abort() }
  }, [])

  useEffect(() => {
    if (demo) return
    const controller = new AbortController()
    let cancelled = false
    void (async () => {
      try {
        const response = await fetch(apiURL + '/api/v1/people?organizationId=' + encodeURIComponent(organizationID), {
          credentials: 'include', signal: controller.signal,
          headers: { 'X-Demo-User-ID': requesterID, 'X-Organization-ID': organizationID },
        })
        if (!response.ok) throw new Error('directory unavailable')
        const result = await response.json() as { people: Recipient[] }
        if (!Array.isArray(result.people)) throw new Error('invalid directory')
        if (!cancelled) {
          const available = result.people.filter(person => person.id !== requesterID)
          setRecipients(available)
          setTarget(current => available.some(person => person.id === current) ? current : '')
        }
      } catch {
        if (!cancelled) { setRecipients([]); setDirectoryError('依頼先を取得できません。組織への所属を確認し、再読み込みしてください。') }
      } finally { if (!cancelled) setLoading(false) }
    })()
    return () => { cancelled = true; controller.abort() }
  }, [apiURL, organizationID, requesterID, demo, generation])

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (sending.current || loading || directoryError || !recipients.some(person => person.id === target)) return
    const form = new FormData(event.currentTarget)
    const end = new Date(String(form.get('deadline')))
    if (!Number.isFinite(end.getTime())) { setError('期限の日付と時刻を入力してください。'); return }
    const payload = JSON.stringify({
      targetUserId: target, type: String(form.get('type')), title: String(form.get('title')).trim(),
      durationMinutes: Number(form.get('duration')), deadlineAt: end.toISOString(),
      syncPreference: String(form.get('sync')), priority: String(form.get('priority')),
    })
    // Keep the exact command/key after a lost response, including its absolute deadline.
    if (!attempt.current || attempt.current.payload !== payload) {
      if (end.getTime() <= Date.now()) { setError('期限は現在より後の日時を指定してください。'); return }
      attempt.current = { payload, key: crypto.randomUUID() }
    }
    sending.current = true
    setSaving(true)
    setError('')
    const controller = new AbortController()
    submission.current = controller
    const timer = setTimeout(() => controller.abort(), 20_000)
    try {
      const response = await fetch(apiURL + '/api/v1/requests', {
        method: 'POST', credentials: 'include', signal: controller.signal,
        headers: { 'Content-Type': 'application/json', 'Idempotency-Key': attempt.current.key, 'X-Demo-User-ID': requesterID, 'X-Organization-ID': organizationID },
        body: attempt.current.payload,
      })
      if (!response.ok) {
        if (response.status === 409) throw new Error('送信キーと内容が一致しません。送信済みの依頼を確認してください。')
        if (response.status === 401 || response.status === 403) throw new Error('送信できません。ログイン状態と、双方が同じ組織に所属していることを確認してください。')
        if (response.status === 422 || response.status === 400) throw new Error('入力内容を確認してください。自己依頼や期限切れの依頼は送信できません。')
        throw new Error('送信結果を確認できません。同じ内容で再送すると重複を防げます。内容を変える前に送信済みを確認してください。')
      }
      const created = await response.json() as { options?: unknown[] }
      if (alive.current) onCreated(Array.isArray(created.options) ? created.options.length : 0)
    } catch (failure) {
      if (alive.current) setError(failure instanceof Error && !['TypeError', 'AbortError'].includes(failure.name)
        ? failure.message : '送信結果を確認できません。同じ内容で再送すると重複を防げます。内容を変える前に送信済みを確認してください。')
    } finally {
      clearTimeout(timer)
      sending.current = false
      if (alive.current) setSaving(false)
    }
  }

  return <div className="modal-backdrop" role="presentation">
    <section className="modal" role="dialog" aria-modal="true" aria-labelledby="request-title">
      <div className="modal-heading"><h2 id="request-title">依頼を作成</h2><button className="close-button" type="button" aria-label="閉じる" disabled={saving} onClick={onClose}>×</button></div>
      {loading ? <p role="status">依頼先を読み込んでいます…</p> : null}
      {directoryError ? <div><p role="alert">{directoryError}</p><button className="secondary-button" type="button" onClick={() => { setLoading(true); setDirectoryError(''); setGeneration(value => value + 1) }}>依頼先を再読み込み</button></div> : null}
      {!loading && !directoryError && recipients.length === 0 ? <p role="status">依頼先となる管理職がいません。組織にメンバーを招待し、管理職の役割を設定してください。</p> : null}
      <form className="request-form" onSubmit={submit}>
        <fieldset className="request-fields" disabled={saving || loading || Boolean(directoryError) || recipients.length === 0}>
          <label>依頼先<select name="target" value={target} onChange={event => setTarget(event.target.value)} required><option value="">管理職を選択</option>{recipients.map(person => <option key={person.id} value={person.id}>{person.displayName}</option>)}</select></label>
          <label>依頼の種類<select name="type" defaultValue="review"><option value="quick_question">質問</option><option value="meeting">相談</option><option value="review">レビュー</option><option value="approval">承認</option><option value="decision">判断</option><option value="async_response">返答</option><option value="urgent_contact">緊急連絡</option></select></label>
          <label>依頼内容<input name="title" defaultValue={demo ? '新API設計レビュー' : ''} placeholder="確認してほしい内容を入力" required maxLength={500} /></label>
          <label>必要時間<select name="duration" defaultValue="15"><option value="5">5分</option><option value="15">15分</option><option value="30">30分</option><option value="60">60分</option></select></label>
          <label>回答期限<input name="deadline" type="datetime-local" defaultValue={deadline} required /></label>
          <p className="field-help">期限は端末のタイムゾーンで入力してください。送信後、選択した相手に通知します。</p>
          <label>回答方法<select name="sync" defaultValue="either"><option value="either">会話・非同期のどちらでも可</option><option value="sync">直接相談したい</option><option value="async">非同期回答でよい</option></select></label>
          <label>重要度<select name="priority" defaultValue="normal"><option value="normal">通常</option><option value="high">高</option><option value="urgent">緊急</option></select></label>
        </fieldset>
        {error ? <p role="alert">{error}</p> : null}
        <div className="modal-actions"><button className="secondary-button" type="button" disabled={saving} onClick={onClose}>キャンセル</button><button className="primary-button" type="submit" disabled={saving || loading || Boolean(directoryError) || !recipients.some(person => person.id === target)}>{saving ? '送信中…' : '候補を生成して送信'}</button></div>
      </form>
    </section>
  </div>
}
