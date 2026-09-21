import { useEffect, useRef, useState } from 'react'
import type { FormEvent } from 'react'

export type MeetingOffer = { id: string; requestId?: string; type: 'meeting' | 'async' | 'delegate' | 'decline'; startAt?: string; endAt?: string; proposedByUserId?: string }
type Identity = { apiURL: string; organizationID: string; actor: string; requestID: string; disabled: boolean }
const headers = (props: Identity) => ({ 'Content-Type': 'application/json', 'X-Demo-User-ID': props.actor, 'X-Organization-ID': props.organizationID })
function useOperation() {
  const alive = useRef(true)
  const lock = useRef(false)
  const controllers = useRef(new Set<AbortController>())
  useEffect(() => { alive.current = true; const pending = controllers.current; return () => { alive.current = false; pending.forEach(controller => controller.abort()) } }, [])
  return {
    isActive: () => alive.current,
    begin: () => { if (lock.current || !alive.current) return null; lock.current = true; const controller = new AbortController(); controllers.current.add(controller); return controller },
    finish: (controller: AbortController) => { controllers.current.delete(controller); lock.current = false },
  }
}

export function CounterproposalForm(props: Identity & { durationMinutes: number; onProposed: (option: MeetingOffer) => void }) {
  const { isActive, begin, finish } = useOperation()
  const [start, setStart] = useState('')
  const [end, setEnd] = useState('')
  const [busy, setBusy] = useState(false)
  const [unknown, setUnknown] = useState(false)
  const [error, setError] = useState('')
  const submit = async (event: FormEvent) => {
    event.preventDefault()
    if (props.disabled) return
    const startAt = new Date(start), endAt = new Date(end)
    if (!Number.isFinite(startAt.getTime()) || !Number.isFinite(endAt.getTime()) || endAt.getTime() - startAt.getTime() !== props.durationMinutes * 60_000) {
      setError(`依頼の所要時間（${props.durationMinutes}分）と同じ長さで指定してください。`); return
    }
    const pending = begin(); if (!pending) return
    setBusy(true); setError('')
    const timeout = setTimeout(() => pending.abort(), 20_000)
    try {
      const response = await fetch(`${props.apiURL}/api/v1/requests/${encodeURIComponent(props.requestID)}/suggest`, {
        method: 'POST', credentials: 'include', headers: headers(props), signal: pending.signal,
        body: JSON.stringify({ startAt: startAt.toISOString(), endAt: endAt.toISOString() }),
      })
      if (!response.ok) {
        if ([400, 401, 403, 404, 409, 422].includes(response.status)) {
          const data = await response.json() as { code?: string }
          if (isActive()) setError(data.code === 'proposal_limit' ? '候補は10件までです。既存の候補から調整するか、依頼を作り直してください。' : '提案できません。期限・所要時間・担当・所属を確認し、一覧を更新してください。')
          return
        }
        throw new Error('unknown')
      }
      const option = await response.json() as MeetingOffer
      if (!option.id || option.requestId !== props.requestID || option.type !== 'meeting' || option.proposedByUserId !== props.actor || Date.parse(option.startAt ?? '') !== startAt.getTime() || Date.parse(option.endAt ?? '') !== endAt.getTime()) throw new Error('invalid acknowledgement')
      if (isActive()) { props.onProposed(option); setUnknown(false); setStart(''); setEnd('') }
    } catch {
      if (isActive()) { setUnknown(true); setError('提案結果を確認できません。同じ日時で再試行できます。日時を変える前に一覧を更新してください。') }
    } finally { clearTimeout(timeout); finish(pending); if (isActive()) setBusy(false) }
  }
  return <form className="suggest-form" onSubmit={submit}>
    <p className="field-help">相手に別の時間を提案します。相手が承認するまで予約は確定しません。所要時間は{props.durationMinutes}分です。</p>
    <label>別の開始時間<input name="suggestStart" type="datetime-local" required value={start} disabled={props.disabled || busy || unknown} onChange={event => setStart(event.target.value)} /></label>
    <label>終了時間<input name="suggestEnd" type="datetime-local" required value={end} disabled={props.disabled || busy || unknown} onChange={event => setEnd(event.target.value)} /></label>
    {error ? <p role="alert">{error}</p> : null}
    <button type="submit" disabled={props.disabled || busy}>{busy ? '提案中…' : '別時間を提案'}</button>
  </form>
}

export function CounterproposalAgreement(props: Identity & { targetID: string; status: string; options: MeetingOffer[]; onConfirmed: (optionID: string) => void }) {
  const { isActive, begin, finish } = useOperation()
  const [busy, setBusy] = useState(false)
  const [uncertainOption, setUncertainOption] = useState('')
  const [error, setError] = useState('')
  const offers = props.options.filter(option => option.type === 'meeting' && option.proposedByUserId === props.targetID && props.actor !== props.targetID && Number.isFinite(Date.parse(option.startAt ?? '')) && Number.isFinite(Date.parse(option.endAt ?? '')))
  const confirm = async (optionID: string) => {
    if (props.disabled || (uncertainOption && uncertainOption !== optionID)) return
    const pending = begin(); if (!pending) return
    setBusy(true); setError('')
    const timeout = setTimeout(() => pending.abort(), 20_000)
    try {
      const response = await fetch(`${props.apiURL}/api/v1/requests/${encodeURIComponent(props.requestID)}/accept`, {
        method: 'POST', credentials: 'include', headers: headers(props), signal: pending.signal, body: JSON.stringify({ optionId: optionID }),
      })
      if (!response.ok) {
        if ([400, 401, 403, 404, 409, 422].includes(response.status)) {
          if (isActive()) setError('確定できません。空き状況・期限・担当が変わっている可能性があります。一覧を更新し、別の候補を相談してください。')
          return
        }
        throw new Error('unknown')
      }
      const result = await response.json() as { id: string; status: string; acceptedOptionId: string }
      if (result.id !== props.requestID || result.status !== 'accepted' || result.acceptedOptionId !== optionID) throw new Error('invalid acknowledgement')
      if (isActive()) props.onConfirmed(optionID)
    } catch {
      if (isActive()) { setUncertainOption(optionID); setError('承認結果を確認できません。同じ提案への再試行は重複しません。別の候補を選ぶ前に一覧を更新してください。') }
    } finally { clearTimeout(timeout); finish(pending); if (isActive()) setBusy(false) }
  }
  if (props.status !== 'suggested' || offers.length === 0) return null
  const format = new Intl.DateTimeFormat('ja-JP', { dateStyle: 'medium', timeStyle: 'short' })
  return <section aria-label="相手からの時間提案">
    <p className="field-help">相手からの提案です。承認時に最新の空き状況を確認し、会議を確定します。</p>
    {offers.map(option => <div className="option-row" key={option.id}>
      <span>時間提案</span><strong>{format.format(new Date(option.startAt!))} — {format.format(new Date(option.endAt!))}</strong>
      <button type="button" disabled={props.disabled || busy || Boolean(uncertainOption && uncertainOption !== option.id)} onClick={() => void confirm(option.id)}>この提案を承認</button>
    </div>)}
    {error ? <p role="alert">{error}</p> : null}
  </section>
}
