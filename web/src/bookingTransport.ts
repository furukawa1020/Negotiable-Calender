// The deadline includes body decoding. Aborting fetch is best-effort: a command
// may already have committed on the server, so callers must not auto-retry it.
export async function fetchBookingResult<T>(
  input: RequestInfo | URL,
  init: Omit<RequestInit, 'signal'>,
  read: (response: Response) => Promise<T>,
  isCurrent: () => boolean,
): Promise<T | undefined> {
  if (!isCurrent()) return
  const controller = new AbortController()
  let timer: ReturnType<typeof setTimeout> | undefined
  const current = () => !controller.signal.aborted && isCurrent()
  const deadline = new Promise<never>((_, reject) => {
    timer = setTimeout(() => {
      // Settle independently of fetch/stream implementations honoring abort.
      reject(new Error('booking outcome unknown: deadline exceeded'))
      controller.abort()
    }, 20_000)
  })
  const operation = async () => {
    const response = await fetch(input, { ...init, credentials: 'include', signal: controller.signal })
    if (!current()) return
    if (!response.ok) throw new Error('booking outcome unconfirmed')
    const value = await read(response)
    if (current()) return value
  }
  try { return await Promise.race([operation(), deadline]) }
  finally { clearTimeout(timer) }
}

export async function readCancellation(response: Response, requestID: string): Promise<true> {
  const value: unknown = await response.json()
  if (!value || typeof value !== 'object' || !('id' in value) || value.id !== requestID
    || !('status' in value) || value.status !== 'cancelled') {
    throw new Error('cancellation acknowledgement invalid')
  }
  return true
}
