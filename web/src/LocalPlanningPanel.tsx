import { useEffect, useRef, useState } from 'react'
import { abortable, languageOptions, localModel, parseRanking, planningFailureMessage, planningPrompt, rankingSchema, samePlanningSource, validatePreview } from './localPlanning'
import type { LocalAvailability, LocalSession, PlanningCandidate, PlanningPreference, PlanningPreview } from './localPlanning'

type Props = { loadPreview: (signal: AbortSignal) => Promise<PlanningPreview> }

export function LocalPlanning({ loadPreview }: Props) {
  const [opened, setOpened] = useState(false)
  const [preview, setPreview] = useState<PlanningPreview | null>(null)
  const [availability, setAvailability] = useState<LocalAvailability>('unavailable')
  const [preference, setPreference] = useState<PlanningPreference>('earlier')
  const [consent, setConsent] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [ranking, setRanking] = useState<PlanningCandidate[]>([])
  const active = useRef<AbortController | null>(null)
  useEffect(() => () => active.current?.abort(), [])
  // Expiring the snapshot also removes displayed AI output, not just its button.
  useEffect(() => {
    if (!preview) return
    const timer = window.setTimeout(() => { active.current?.abort(); setPreview(null); setRanking([]); setConsent(false); setBusy(false); setError('候補の有効期限が切れました。一度閉じて候補を取得し直してください。') }, Math.max(0, Date.parse(preview.expiresAt) - Date.now()))
    return () => window.clearTimeout(timer)
  }, [preview])
  const close = () => { active.current?.abort(); active.current = null; setOpened(false); setPreview(null); setConsent(false); setRanking([]); setBusy(false); setError('') }
  const open = async () => {
    active.current?.abort()
    const controller = new AbortController(); active.current = controller
    setOpened(true); setBusy(true); setError(''); setRanking([]); setConsent(false)
    const timer = window.setTimeout(() => controller.abort(), 15000)
    try {
      const fresh = validatePreview(await abortable(loadPreview(controller.signal), controller.signal))
      const model = localModel()
      const status = model ? await abortable(model.availability(languageOptions).catch(() => 'unavailable' as const), controller.signal) : 'unavailable'
      if (controller.signal.aborted) return
      setPreview(validatePreview(fresh)); setAvailability(status)
    } catch (error) {
      if (!controller.signal.aborted) setError(planningFailureMessage(error, '候補を確認できません。依頼・同期状態を更新して再試行してください。'))
    } finally { window.clearTimeout(timer); if (active.current === controller) { setBusy(false); if (controller.signal.aborted) setError('候補確認を中断しました。閉じて再試行してください。') } }
  }
  const compare = async () => {
    if (!consent || !preview || busy || availability === 'unavailable') return
    const model = localModel(); if (!model) return
    const controller = new AbortController(); active.current = controller
    let session: LocalSession | undefined
    const destroy = () => { try { session?.destroy() } catch { /* sanitized */ } }
    controller.signal.addEventListener('abort', destroy, { once: true })
    setBusy(true); setError(''); setRanking([])
    const timer = window.setTimeout(() => controller.abort(), 90000)
    try {
      validatePreview(preview)
      // create() happens in the explicit click activation, before network awaits.
      const createdSession = await abortable(model.create({ ...languageOptions, signal: controller.signal }).then((created) => {
        if (controller.signal.aborted) { created.destroy(); throw new Error('aborted') }
        return created
      }), controller.signal)
      session = createdSession
      if (controller.signal.aborted) return
      const before = validatePreview(await abortable(loadPreview(controller.signal), controller.signal))
      if (!samePlanningSource(preview, before)) throw new Error('source changed')
      if (controller.signal.aborted) return
      const raw = await abortable(createdSession.prompt(planningPrompt(preview, preference), { signal: controller.signal, responseConstraint: rankingSchema(preview) }), controller.signal)
      if (controller.signal.aborted) return
      const selected = parseRanking(raw, preview)
      const after = validatePreview(await abortable(loadPreview(controller.signal), controller.signal))
      if (!samePlanningSource(preview, after) || controller.signal.aborted) throw new Error('source changed')
      setRanking(selected)
    } catch (error) {
      if (!controller.signal.aborted) setError(planningFailureMessage(error, 'AI の比較を完了できませんでした。候補や空き状況が変わった可能性もあります。下の通常の候補をご利用ください。'))
    } finally {
      window.clearTimeout(timer)
      controller.signal.removeEventListener('abort', destroy)
      destroy()
      if (active.current === controller) { setBusy(false); setConsent(false); if (controller.signal.aborted) setError('AI 処理を中断しました。通常の候補をご利用ください。') }
    }
  }
  const label = (candidate: PlanningCandidate) => `${new Date(candidate.startAt).toLocaleString('ja-JP')} — ${new Date(candidate.endAt).toLocaleTimeString('ja-JP', { hour: '2-digit', minute: '2-digit' })}`
  return <section className="confirmed-meeting local-planning" aria-label="端末内 AI による候補比較">
    {!opened ? <button type="button" onClick={() => void open()}>端末内 AI で候補を比べる</button> : <>
      <strong>Google Gemini Nano · 端末内の候補比較</strong>
      <p>候補の時刻・仮 ID・希望条件だけを、この端末の AI で処理します。予定名・参加者・場所は渡しません。クラウド AI への送信・自動切替・予約確定は行いません。</p>
      <p>対応する Chrome・端末が必要です。初回はモデルのダウンロードが必要な場合があり、通信量と端末の保存領域を使います。AI 推論の API 利用料は発生しません。</p>
      {busy ? <p role="status">候補確認・端末内処理中です。モデルの初回準備には時間がかかることがあります。</p> : null}
      {preview ? <>
        <p>処理する候補（端末のタイムゾーンで表示）：</p>
        <ul>{preview.candidates.map((c) => <li key={c.id}>{c.id} · {label(c)}</li>)}</ul>
        <label>希望条件<select value={preference} disabled={busy} onChange={(e) => { setPreference(e.target.value as PlanningPreference); setConsent(false); setRanking([]) }}><option value="earlier">早い時間を優先</option><option value="later">遅い時間を優先</option></select></label>
        {availability === 'unavailable' ? <p>この環境では端末内 AI を利用できません。下の通常の候補をそのまま使えます（AI ではありません）。</p> : <>
          <label><input type="checkbox" checked={consent} disabled={busy} onChange={(e) => setConsent(e.target.checked)} />表示した候補を端末内 AI で処理し、必要なモデルをダウンロードすることに同意します</label>
          <button type="button" disabled={!consent || busy} onClick={() => void compare()}>同意して端末内で比較</button>
        </>}
      </> : null}
      {ranking.length ? <div role="status"><strong>AI の比較結果（予約ではありません）</strong><ol>{ranking.map((c) => <li key={c.id}>{label(c)}</li>)}</ol><p>確定する場合は下の通常の承認操作を使ってください。その時点の競合・公開状態をサーバーで再確認します。</p></div> : null}
      {error ? <p role="alert">{error}</p> : null}
      <button type="button" onClick={close}>{busy ? '中断して閉じる' : '閉じる'}</button>
    </>}
  </section>
}
