import { useEffect, useState } from 'react'

type Props = { mode: string; connection: { sourceFresh?: boolean; lastSyncedAt?: string; lastAttemptAt?: string; nextAttemptAt?: string; lastErrorCode?: string; reconnectRequired: boolean } }

function recoveryMessage(code: string) {
  switch (code) {
    case 'rate_limited': return 'Googleの利用制限に達しました。時間をおいて同期してください。再接続は不要です。'
    case 'provider_configuration': return 'Google連携のアプリ設定に問題があります。運営者にお問い合わせください。再接続では解消しません。'
    case 'provider_denied': return 'Googleが予定の読み取りを拒否しました。原因を確認するため、運営者にお問い合わせください。'
    default: return '時間をおいて同期を再試行してください。'
  }
}

export function CalendarSyncStatus({ mode, connection }: Props) {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => { const timer = window.setInterval(() => setNow(Date.now()), 60000); return () => window.clearInterval(timer) }, [])
  const synced = connection.lastSyncedAt ? Date.parse(connection.lastSyncedAt) : NaN
  const stale = connection.sourceFresh === false || !Number.isFinite(synced) || synced > now || now - synced >= 30 * 60000
  const format = (value: string) => new Date(value).toLocaleString('ja-JP')
  return <div>
    <span>{connection.reconnectRequired ? 'Calendarの再接続が必要です' : mode === 'external' ? 'Calendar 定期同期を設定済み' : mode === 'background' ? 'Calendar ローカル自動同期' : 'Calendar 自動同期は停止中（手動同期は利用できます）'}</span>
    <p>{connection.lastSyncedAt ? `最終成功 ${format(connection.lastSyncedAt)}` : '同期成功はまだありません。'}</p>
    {stale && !connection.reconnectRequired ? <p role="status">同期の鮮度を確認できません。予定が変わっている可能性があります。同期の状態を確認してください。</p> : null}
    {connection.lastAttemptAt ? <p>最終試行 {format(connection.lastAttemptAt)}</p> : null}
    {!connection.reconnectRequired && connection.lastErrorCode ? <p role="status">前回の同期に失敗しました。{recoveryMessage(connection.lastErrorCode)}</p> : null}
    {!connection.reconnectRequired && connection.nextAttemptAt && mode !== 'off' ? <p>再同期の対象時刻 {format(connection.nextAttemptAt)} 以降、次の起動で処理します。定期起動は遅延する場合があります。</p> : null}
  </div>
}
