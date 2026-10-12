import { useEffect, useState } from 'react'

// One bounded timer, plus resume reconciliation. No API polling or mutations.
export function useMeetingClock(start: number, end: number, active: boolean) {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    if (!active || !Number.isFinite(start) || !Number.isFinite(end) || end < start) return
    let timer: ReturnType<typeof setTimeout> | undefined
    const refresh = () => {
      clearTimeout(timer)
      const current = Date.now()
      setNow(current)
      const next = [start, end].find(boundary => boundary > current)
      if (next !== undefined) timer = setTimeout(refresh, Math.min(next - current, 2_147_483_647))
    }
    refresh()
    window.addEventListener('focus', refresh)
    document.addEventListener('visibilitychange', refresh)
    return () => {
      clearTimeout(timer)
      window.removeEventListener('focus', refresh)
      document.removeEventListener('visibilitychange', refresh)
    }
  }, [start, end, active])
  return now
}
