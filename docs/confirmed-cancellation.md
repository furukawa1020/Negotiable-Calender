# Confirmed meeting cancellation (#106, Epic #102)

`POST /api/v1/requests/{requestId}/cancel-confirmed` requires an authenticated
request participant and JSON `{ "optionId": "currently-confirmed-option" }`.
Both requester and target can cancel a valid future confirmed meeting. A different
selection, pending/non-meeting request or started meeting returns 409. Outsiders
receive 404; missing identity returns 401. The original pending-request cancel
route remains requester-only and cannot cancel accepted meetings.

The storage transaction changes status to cancelled, releases the app reservation,
creates one counterpart in-app notification and records the actor in the audit.
If either effect cannot be saved, the entire operation rolls back. Deterministic
effect IDs and retaining acceptedOptionId allow retries of the same cancellation
to return 200 without duplicating effects, including after the original start.
Both implementations preserve the original selected option as historical metadata.

Firestore checks all participant account-deletion fences and touches the same
per-person controls as acceptance. PostgreSQL uses a SERIALIZABLE transaction and
request row lock; serialization failures return booking_conflict for a fresh retry.
An overlapping acceptance racing with cancellation may conflict; a subsequent
attempt sees the released slot. Cancelled requests do not reserve candidate ranges
and cannot export ICS. There is no new reservation table or migration.

The inbox and sent views require a second explicit confirmation. Failed requests
leave the confirmed state intact. Both views update after a successful response.
Notifications are durable app records, not email/push delivery or an external outbox.
Acceptance and other older routes retain their existing best-effort side effects;
this change does not guarantee global ordering between all lifecycle notifications.

## Limits

- Imported ICS / Google events are **not** deleted or updated. UI and notification
  explain that users must remove these themselves. No Google write scopes added.
- Changing a confirmed time while preserving the old reservation until replacement
  succeeds is handled separately by [meeting rescheduling](meeting-reschedule.md).
- The production demo identity mode is not proof of real OAuth acceptance (#76).
- This completes the cancellation slice, not Epic #102 or scheduled sync #89.

Tests cover both actors, stale selection, outsiders, start boundary, concurrent
retries, effect rollback, released-slot reuse, HTTP input/error handling and UI
confirmation/retry. Database tests require PostgreSQL / Firestore emulator in CI;
local Go tests without those services skip their integration cases.
