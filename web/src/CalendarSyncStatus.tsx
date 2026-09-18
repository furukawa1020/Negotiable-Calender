import { useEffect, useState } from 'react'

type Props = { mode: string; connection: { sourceFresh?: boolean; lastSyncedAt?: string; lastAttemptAt?: string; nextAttemptAt?: string; lastErrorCode?: string; reconnectRequired: boolean } }

export function CalendarSyncStatus({ mode, connection }: Props) {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => { const timer = window.setInterval(() => setNow(Date.now()), 60000); return () => window.clearInterval(timer) }, [])
  const synced = connection.lastSyncedAt ? Date.parse(connection.lastSyncedAt) : NaN
  const stale = connection.sourceFresh === false || !Number.isFinite(synced) || synced > now || now - synced >= 30 * 60000
  const format = (value: string) => new Date(value).toLocaleString('ja-JP')
  return <div>
    <span>{connection.reconnectRequired ? 'Calendarの再接続が必要です' : mode === 'external' ? 'Calendar 定期同期を設定済み' : mode === 'background' ? 'Calendar ローカル自動同期' : 'Calendar 自動同期は停止中（手動同期は利用できます）'}</span>
    <p>{connection.lastSyncedAt ? `最終成功 ${format(connection.lastSyncedAt)}` : '同期成功はまだありません。'}</p>
    {stale && !connection.reconnectRequired ? <p role="status">同期の鮮度を確認できません。予定が変わっている可能性があります。同期・再接続を確認してください。</p> : null}
    {connection.lastAttemptAt ? <p>最終試行 {format(connection.lastAttemptAt)}</p> : null}
    {!connection.reconnectRequired && connection.lastErrorCode ? <p role="status">前回の同期に失敗しました（{connection.lastErrorCode}）。</p> : null}
    {!connection.reconnectRequired && connection.nextAttemptAt && mode !== 'off' ? <p>再同期の対象時刻 {format(connection.nextAttemptAt)} 以降、次の起動で処理します。定期起動は遅延する場合があります。</p> : null}
  </div>
}
