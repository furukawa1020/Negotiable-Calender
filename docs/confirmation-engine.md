# Atomic coordination confirmation (#104, Epic #102)

Accept is now a storage transaction, not a status toggle. It checks ownership,
suggested state, meeting type, option/request identity, valid timestamps, future
start and request deadline. Every instant in the meeting must be covered by
unexpired available/limited, request-open public projection segments. Unknown,
gaps, closed, expired and contradictory overlapping segments fail closed.

Both requester and target are checked against accepted meetings in either role,
across organizations. Intervals are half-open: adjacent meetings are allowed.
No private event title, meeting count or conflicting participant details appear
in the error. Candidate generation also excludes already-confirmed ranges;
acceptance rechecks because a candidate list can become stale immediately.

## Firestore

The transaction reads/writes a per-person coordinationConfirmation control in
users/{id}/projectionControls. Every accept touches both people's controls.
Concurrent requests therefore cannot both pass an empty conflict check. The
request, all account deletion fences, projection disconnect/publication gate,
policy revision and private-input revision are read in the same transaction.
Uncommitted or changed source/publication revisions are rejected.

No extra retained booking records or migration/index is introduced; accepted
requests remain the reservation source of truth. Account deletion already cleans
the controls and requests. Do not run old unfenced acceptance code concurrently.
Initial scans are deliberately capped at 5,000 requests per role/person and
10,000 projection rows for the target. Exceeding a cap rejects confirmation rather
than silently truncating checks. Indexed interval storage/load testing is a future
scaling task, not a claim that this implementation is ready for arbitrary volume.

## PostgreSQL

A SERIALIZABLE transaction reads the request, selected option, accepted-request
predicates for both people and committed projection rows, then updates the request.
Serialization/deadlock failures return booking_conflict for an explicit retry;
the aborted transaction's result is never used. See PostgreSQL's
[serialization failure handling](https://www.postgresql.org/docs/16/mvcc-serialization-failure-handling.html).
This covers concurrent acceptance and a consistent published-row snapshot. It
does not add Firestore-style private-input/publication revision fencing to the
PostgreSQL sync pipeline; parity there remains a separate concern.

## API and UI

HTTP 409 has one of candidate_invalid, candidate_expired, availability_changed,
or booking_conflict. The UI keeps the request unchanged and points to another
time or refreshing sync. Repeating the same accepted option returns 200 without
duplicating audit/notification creation. Different options cannot overwrite an
accepted choice. Transport failures after commit are therefore safe to retry.
New acceptance commits its requester app notification and audit in the same storage
transaction as the reservation (#113). A failed effect insert rolls everything back;
stable insert-only IDs fail closed on collisions. Concurrent/repeated acceptance
does not duplicate effects or reset an existing notification's read state. The HTTP
handler never writes a second acceptance notification/audit after commit.

This is durable app-inbox persistence, not an external-delivery outbox or an email,
push, or Google Calendar delivery guarantee. Pre-existing accepted requests are not
backfilled: replay preserves their historical state, including any legacy missing
effects. Deploy both storage and HTTP changes together; old revisions must be drained
before relying on the new guarantee. No schema migration or bulk repair is performed.
Decline/async/delegation still use their existing best-effort notification path.
The separate [confirmed cancellation](confirmed-cancellation.md) operation likewise
persists its counterpart app notification and audit in the cancellation transaction.

Tests exercise real Firestore emulator and PostgreSQL concurrent transactions,
cross-organization/opposite-role conflicts, adjacent meetings, pending/dirty
publication, time/coverage validation, unchanged state on error and API retries.
Effect tests inject notification/audit insertion collisions in both databases,
verify complete rollback, race same-request acceptance, and replay after marking
the resulting notification read. Firestore deletion fences remain in the transaction.

These guarantees concern app-confirmed requests and the currently imported public
availability snapshot. They do not reserve Google Calendar or detect an external
Google change that has not yet synced. Live OAuth (#76), reliable scheduled sync
(#89), and broader negotiation completion (#102) remain open.
