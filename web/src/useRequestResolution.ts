import { useEffect, useRef, useState } from 'react'

type Action = 'async' | 'decline' | 'cancel'
type Result = { id: string; status: string; asyncMessage?: string }
const unknownOutcome = '結果を確認できません。同じ操作・同じ回答文で再試行できます。内容を変える前に一覧を更新して確認してください。'

export function useRequestResolution(apiURL: string, organizationID: string, identity: string) {
  const scopeKey = JSON.stringify([apiURL, organizationID, identity])
  const scope = useRef({ active: true, locks: new Set<string>(), controllers: new Set<AbortController>() })
  const [pending, setPending] = useState<Map<string, symbol>>(new Map())
  useEffect(() => {
    const current = { active: true, locks: new Set<string>(), controllers: new Set<AbortController>() }
    scope.current = current
    return () => { current.active = false; current.controllers.forEach(controller => controller.abort()) }
  }, [scopeKey])

  const resolve = async (id: string, actor: string, action: Action, message?: string): Promise<Result | undefined> => {
    const current = scope.current
    if (!current.active || current.locks.has(id)) return
    current.locks.add(id)
    const key = JSON.stringify([scopeKey, id])
    const token = Symbol('resolution')
    setPending(previous => new Map(previous).set(key, token))
    const controller = new AbortController()
    current.controllers.add(controller)
    const timeout = setTimeout(() => controller.abort(), 20_000)
    try {
      const response = await fetch(`${apiURL}/api/v1/requests/${encodeURIComponent(id)}/${action}`, {
        method: 'POST', credentials: 'include', signal: controller.signal,
        headers: { 'Content-Type': 'application/json', 'X-Demo-User-ID': actor, 'X-Organization-ID': organizationID },
        body: action === 'async' ? JSON.stringify({ message: message?.trim() }) : undefined,
      })
      if (!response.ok) {
        let code = ''
        try { code = (await response.json() as { code?: string }).code ?? '' } catch { /* no response details */ }
        if (response.status === 409) throw new Error(code === 'request_resolution_expired'
          ? '回答期限を過ぎています。新しい期限で依頼し直してください。'
          : '別の回答・承認・取り下げが先に保存されています。一覧を更新して確認してください。')
        if ([401, 403, 404].includes(response.status)) throw new Error('この依頼を操作できません。ログイン状態・組織・依頼の宛先を確認してください。')
        if ([400, 422].includes(response.status)) throw new Error('入力内容を確認してください。回答は空白以外の500文字以内で入力してください。')
        throw new Error(unknownOutcome)
      }
      const result = await response.json() as Result
      const expected = { async: 'async', decline: 'declined', cancel: 'cancelled' }[action]
      if (result.id !== id || result.status !== expected || (action === 'async' && result.asyncMessage !== message?.trim())) throw new Error(unknownOutcome)
      if (current.active) return result
    } catch (error) {
      if (current.active) throw new Error(error instanceof Error && error.name === 'Error' ? error.message : unknownOutcome, { cause: error })
    } finally {
      clearTimeout(timeout)
      current.controllers.delete(controller)
      current.locks.delete(id)
      // Clear abandoned scopes too, without unlocking a newer command for the
      // same request after the user switches away and back. No payload is applied.
      setPending(previous => {
        if (previous.get(key) !== token) return previous
        const next = new Map(previous); next.delete(key); return next
      })
    }
  }
  return { resolve, pending: (id: string) => pending.has(JSON.stringify([scopeKey, id])) }
}
