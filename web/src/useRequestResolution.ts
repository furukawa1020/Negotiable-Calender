import { useEffect, useRef, useState } from 'react'
import { fetchBookingResult } from './bookingTransport'
import { readAcceptance } from './bookingResponse'

type Action = 'async' | 'decline' | 'cancel'
type Result = { id: string; status: string; asyncMessage?: string; acceptedOptionId?: string }
type Command = { action: Action; message?: string } | { action: 'accept'; optionID: string }
const unknownOutcome = '結果を確認できません。同じ操作・同じ回答文で再試行できます。内容を変える前に一覧を更新して確認してください。'
const unknownAcceptance = '依頼を更新できませんでした。最新状態を確認してください。'
class CommandError extends Error {}

async function readCommandError(response: Response, acceptance: boolean): Promise<never> {
  if (response.status === 409) {
    let code = ''
    try {
      const value: unknown = await response.json()
      if (value && typeof value === 'object' && 'code' in value && typeof value.code === 'string') code = value.code
    } catch { /* use a static, privacy-safe conflict message */ }
    if (acceptance) {
      const messages: Record<string, string> = {
        candidate_expired: 'この候補は開始済み、または依頼の期限外です。新しい日時で依頼・提案してください。',
        candidate_invalid: 'この候補は会議として確定できません。別の時間を提案してください。',
        availability_changed: '公開された対応可能時間が変わったか、同期を確認できません。同期・更新後に別の時間を提案してください。',
        booking_conflict: '重なる確定済みの調整、または同時更新を検出しました。更新して確認し、必要なら別の時間を提案してください。',
      }
      throw new CommandError(Object.hasOwn(messages, code) ? messages[code] : '依頼の状態が変わりました。更新して確認してください。')
    }
    throw new CommandError(code === 'request_resolution_expired'
      ? '回答期限を過ぎています。新しい期限で依頼し直してください。'
      : '別の回答・承認・取り下げが先に保存されています。一覧を更新して確認してください。')
  }
  if ([401, 403, 404].includes(response.status)) throw new CommandError('この依頼を操作できません。ログイン状態・組織・依頼の宛先を確認してください。')
  if ([400, 422].includes(response.status)) throw new CommandError(acceptance
    ? '承認する候補を確認し、一覧を更新してください。'
    : '入力内容を確認してください。回答は空白以外の500文字以内で入力してください。')
  throw new CommandError(acceptance ? unknownAcceptance : unknownOutcome)
}

export function useRequestResolution(apiURL: string, organizationID: string, identity: string) {
  const scopeKey = JSON.stringify([apiURL, organizationID, identity])
  const scope = useRef({ active: true, locks: new Set<string>(), controllers: new Set<AbortController>() })
  const [pending, setPending] = useState<Map<string, symbol>>(new Map())
  useEffect(() => {
    const current = { active: true, locks: new Set<string>(), controllers: new Set<AbortController>() }
    scope.current = current
    return () => { current.active = false; current.controllers.forEach(controller => controller.abort()) }
  }, [scopeKey])

  const execute = async (id: string, actor: string, command: Command, isCurrent = () => true): Promise<Result | undefined> => {
    const current = scope.current
    const active = () => current.active && isCurrent()
    if (!active() || current.locks.has(id)) return
    const { action } = command
    current.locks.add(id)
    const key = JSON.stringify([scopeKey, id])
    const token = Symbol('resolution')
    setPending(previous => new Map(previous).set(key, token))
    const controller = new AbortController()
    current.controllers.add(controller)
    try {
      return await fetchBookingResult(`${apiURL}/api/v1/requests/${encodeURIComponent(id)}/${action}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-Demo-User-ID': actor, 'X-Organization-ID': organizationID },
        body: command.action === 'accept' ? JSON.stringify({ optionId: command.optionID })
          : command.action === 'async' ? JSON.stringify({ message: command.message?.trim() }) : undefined,
      }, async response => {
        if (command.action === 'accept') {
          await readAcceptance(response, id, command.optionID)
          return { id, status: 'accepted', acceptedOptionId: command.optionID }
        }
        const result = await response.json() as Result
        const expected = { async: 'async', decline: 'declined', cancel: 'cancelled' }[command.action]
        if (!result || result.id !== id || result.status !== expected || (command.action === 'async' && result.asyncMessage !== command.message?.trim())) throw new Error(unknownOutcome)
        return result
      }, active, { signal: controller.signal, readError: response => readCommandError(response, action === 'accept') })
    } catch (error) {
      if (active()) throw new Error(error instanceof CommandError ? error.message : action === 'accept' ? unknownAcceptance : unknownOutcome, { cause: error })
    } finally {
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
  return {
    resolve: (id: string, actor: string, action: Action, message?: string) => execute(id, actor, { action, message }),
    accept: (id: string, actor: string, optionID: string, isCurrent: () => boolean) => execute(id, actor, { action: 'accept', optionID }, isCurrent),
    pending: (id: string) => pending.has(JSON.stringify([scopeKey, id])),
  }
}
