import { useEffect, useRef, useState } from 'react'
import type { FormEvent } from 'react'

type Person = { id: string; displayName: string }
type Props = { apiURL: string; organizationID: string; actor: string; requestID: string; requesterID: string; disabled: boolean; onDone: (name: string) => void }
const uncertain = '引継ぎ結果を確認できません。同じ相手への再試行は重複しません。相手を変える前に一覧を更新してください。'

export function RequestHandoff({ apiURL, organizationID, actor, requestID, requesterID, disabled, onDone }: Props) {
  const [open, setOpen] = useState(false)
  const [people, setPeople] = useState<Person[]>([])
  const [target, setTarget] = useState('')
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const [unknown, setUnknown] = useState(false)
  const alive = useRef(true)
  const lock = useRef(false)
  const controllers = useRef(new Set<AbortController>())
  useEffect(() => { alive.current = true; const pending = controllers.current; return () => { alive.current = false; pending.forEach(controller => controller.abort()) } }, [])

  const load = async () => {
    if (lock.current) return
    lock.current = true; setOpen(true); setLoading(true); setError('')
    const controller = new AbortController(); controllers.current.add(controller)
    const timeout = setTimeout(() => controller.abort(), 20_000)
    try {
      const response = await fetch(`${apiURL}/api/v1/people?organizationId=${encodeURIComponent(organizationID)}`, {
        credentials: 'include', signal: controller.signal, headers: { 'X-Demo-User-ID': actor, 'X-Organization-ID': organizationID },
      })
      if (!response.ok) throw new Error('directory unavailable')
      const data = await response.json() as { people: Person[] }
      if (!Array.isArray(data.people)) throw new Error('invalid directory')
      if (alive.current) {
        const available = data.people.filter(person => person.id !== actor && person.id !== requesterID)
        setPeople(available); setTarget(current => available.some(person => person.id === current) ? current : '')
      }
    } catch {
      if (alive.current) { setPeople([]); setTarget(''); setError('引継ぎ先を取得できません。再読み込みしてください。') }
    } finally { clearTimeout(timeout); controllers.current.delete(controller); lock.current = false; if (alive.current) setLoading(false) }
  }

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    if (disabled || lock.current || !people.some(person => person.id === target)) return
    lock.current = true; setSaving(true); setError('')
    const controller = new AbortController(); controllers.current.add(controller)
    const timeout = setTimeout(() => controller.abort(), 20_000)
    try {
      const response = await fetch(`${apiURL}/api/v1/requests/${encodeURIComponent(requestID)}/delegate`, {
        method: 'POST', credentials: 'include', signal: controller.signal,
        headers: { 'Content-Type': 'application/json', 'X-Demo-User-ID': actor, 'X-Organization-ID': organizationID },
        body: JSON.stringify({ delegateUserId: target }),
      })
      if (!response.ok) {
        if ([400, 401, 403, 404, 409].includes(response.status)) {
          if (alive.current) setError('引継ぎできません。期限・所属・最新の担当を一覧で確認してください。引継ぎは1回までです。')
          return
        }
        throw new Error('unknown outcome')
      }
      const result = await response.json() as { id: string; handedOff: boolean; delegatedUserId: string }
      if (result.id !== requestID || !result.handedOff || result.delegatedUserId !== target) throw new Error('unknown outcome')
      if (alive.current) onDone(people.find(person => person.id === target)!.displayName)
    } catch {
      if (alive.current) { setUnknown(true); setError(uncertain) }
    } finally { clearTimeout(timeout); controllers.current.delete(controller); lock.current = false; if (alive.current) setSaving(false) }
  }

  if (!open) return <button type="button" disabled={disabled} onClick={() => void load()}>担当を引き継ぐ</button>
  return <form className="delegate-form" onSubmit={submit}>
    <p className="field-help">依頼内容と期限を共有し、担当を変更します。元の会議候補は破棄され、引継ぎ後はこの依頼を閲覧・回答できなくなります。引継ぎは1回までです。</p>
    {loading ? <p role="status">引継ぎ先を読み込んでいます…</p> : null}
    {!loading && people.length === 0 ? <p>選択できる別の管理職がいません。組織のメンバー設定を確認してください。</p> : null}
    <label>引継ぎ先<select aria-label="引継ぎ先" required value={target} disabled={disabled || loading || saving || unknown} onChange={event => setTarget(event.target.value)}><option value="">管理職を選択</option>{people.map(person => <option key={person.id} value={person.id}>{person.displayName}</option>)}</select></label>
    {error ? <p role="alert">{error}</p> : null}
    <button type="submit" disabled={disabled || loading || saving || !target}>{saving ? '引継ぎ中…' : 'この相手へ引き継ぐ'}</button>
    {!unknown ? <button type="button" disabled={disabled || loading || saving} onClick={() => void load()}>引継ぎ先を再読み込み</button> : null}
  </form>
}
