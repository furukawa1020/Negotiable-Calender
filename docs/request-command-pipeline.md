# Request command pipeline

Issue #181, part of #102.

Inbox acceptance now shares useRequestResolution with decline, asynchronous
response and unconfirmed cancellation. One synchronous lock per request prevents
duplicate or competing commands until the current attempt settles. Different
requests retain independent pending states; the global respondingRequestID is
removed. Pending tokens and account/workspace generations prevent an obsolete
attempt from unlocking a newer attempt after a switch away and back.

The shared booking transport bounds fetch, success JSON and conflict JSON to
20 seconds. Its deadline settles independently of fetch honoring abort. Scope
cleanup also aborts and settles immediately, with listeners/timers released.
An immediate account-lifetime predicate protects acceptance before React effect
cleanup and before decoding a late response. Existing list/mutation fences remain.

Acknowledgements are still validated, and conflict messages remain static and
privacy-safe. Unknown codes (including inherited property names) use generic
guidance; raw server or transport text is not shown. No automatic retries occur.
A timeout or browser abort does not imply server rollback. The user should check
the latest state before another choice; transactional conflict/idempotency checks
remain the authoritative protection.

This lock covers these four request actions within one hook scope, not separate
rescheduling/counterproposal/handoff components, other browser tabs or clients.
Those retain their existing backend and component protections. Google scopes,
real bookings and billable infrastructure are unchanged.

Tests cover all four actions at response/success-body/error-body stalls,
synchronous duplicates and competing actions, independent rows, abort-insensitive
transport, unmount, workspace return, immediate account invalidation, conflict
guidance and private-error suppression. App tests verify timeout/retry and
independent pending controls across two rows.
