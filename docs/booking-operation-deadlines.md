# Confirmed booking operation deadlines

Issue #175, part of #102.

Rescheduling (propose/accept/decline/withdraw), confirmed cancellation, and ICS
download have a 20-second browser deadline covering both fetch and response-body
decoding. The timer is cleared on success, rejection, or timeout. Timeout aborts
the transport and independently settles the caller even if a transport or body
reader ignores AbortSignal. Late results cannot update state, download a file,
or clear the busy/error state of a newer retry.

A client timeout is **not** evidence that a server mutation failed or rolled
back. There is no automatic retry. The UI retains the existing booking and asks
the user to refresh and verify the current state before retrying. An unchanged
reschedule proposal keeps its proposal ID after failure. Confirmed cancellation
updates local state only after decoding a matching request ID with cancelled
status; malformed or unrelated successful responses remain unconfirmed.

Existing account/workspace generation guards still apply before body decoding
and before any state or download effect. This change does not add Google write
permissions, remote calendar updates, server transaction deadlines, or a global
lock across different booking operations.

Regression coverage in BookingLifecycle.test.tsx exercises response and body
stalls for all three endpoints, transport abort, no automatic retries, late
completion during a newer retry, successful recovery, and invalid cancellation
acknowledgements. bookingTransport.test.ts covers credential/header preservation,
HTTP/network/JSON failures, obsolete scopes, and timer cleanup. Existing lifecycle
and proposal-idempotency tests remain required.
