// The deadline includes body decoding. Aborting fetch is best-effort: a command
// may already have committed on the server, so callers must not auto-retry it.
export async function fetchBookingResult<T>(
  input: RequestInfo | URL,
  init: Omit<RequestInit, 'signal'>,
  read: (response: Response) => Promise<T>,
  isCurrent: () => boolean,
  options: { signal?: AbortSignal; readError?: (response: Response) => Promise<never> } = {},
): Promise<T | undefined> {
  if (!isCurrent()) return
  const controller = new AbortController()
  let timer: ReturnType<typeof setTimeout> | undefined
  let interrupt = () => {}
  const current = () => !controller.signal.aborted && isCurrent()
  const deadline = new Promise<never>((_, reject) => {
    interrupt = () => {
      // Settle independently of fetch/stream implementations honoring abort.
      reject(new Error('booking outcome unknown: deadline exceeded'))
      controller.abort()
    }
    timer = setTimeout(interrupt, 20_000)
    options.signal?.addEventListener('abort', interrupt, { once: true })
    if (options.signal?.aborted) interrupt()
  })
  const operation = async () => {
    if (!current()) return
    const response = await fetch(input, { ...init, credentials: 'include', signal: controller.signal })
    if (!current()) return
    if (!response.ok) {
      if (options.readError) return options.readError(response)
      throw new Error('booking outcome unconfirmed')
    }
    const value = await read(response)
    if (current()) return value
  }
  try { return await Promise.race([operation(), deadline]) }
  finally { clearTimeout(timer); options.signal?.removeEventListener('abort', interrupt) }
}

export async function readCancellation(response: Response, requestID: string): Promise<true> {
  const value: unknown = await response.json()
  if (!value || typeof value !== 'object' || !('id' in value) || value.id !== requestID
    || !('status' in value) || value.status !== 'cancelled') {
    throw new Error('cancellation acknowledgement invalid')
  }
  return true
}
