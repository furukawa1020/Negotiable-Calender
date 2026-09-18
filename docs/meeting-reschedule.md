# Atomic meeting rescheduling (#108, Epic #102)

Both request principals can propose a different start for an accepted, future
meeting. The original acceptedOptionId remains the only reservation while a
proposal is pending. A proposal is not a hold on the new time. Only the counterpart
may accept or decline; only the proposer may withdraw. One proposal can be active.

`POST /api/v1/requests/{requestId}/reschedule` takes action (propose, accept,
decline, withdraw), proposalId, expectedOptionId and, for propose, startAt (RFC3339).
The proposal ID is a client-generated unique 8–80 character ASCII alphanumeric,
hyphen or underscore ID. The UI keeps this ID for retries of the same submission.
The original duration and deadline are retained; a new end after that deadline,
an unchanged time, a started original/new meeting and stale selection fail closed.

## Atomicity and concurrency

- Proposals and outcomes persist their counterpart in-app notification and audit
  in the same storage transaction. Failed writes roll back all changes.
- Acceptance shares the ordinary confirmation engine's transactional conflict and
  public-availability checks. It checks both principals, in either role and across
  organizations, while excluding this request's own original reservation.
- Successful acceptance swaps acceptedOptionId in place. Old and proposed options
  stay as bounded history. There is never an intermediate unreserved state.
- Firestore shares per-person confirmation controls, reads all account-deletion
  fences, and checks the publication/private-input gate. PostgreSQL uses row locking
  and SERIALIZABLE isolation. Serialization failures return 409 for a fresh retry.
- Cancellation uses the expected current option; a stale cancel cannot cancel a
  newly accepted replacement. A cancelled meeting cannot accept a pending proposal.
- Same pending proposal / terminal outcome retries return success without effects.
  Altered payloads, self-approval and reused historic IDs are rejected. After a newer
  proposal replaces terminal metadata, an older retry returns conflict, never reapplies.
- Decline/withdraw leave the original reservation unchanged. Pending proposals do
  not expire in a background worker; stale acceptance is rejected by time checks.

## Persistence and limits

Firestore embeds current proposal metadata in the existing request document.
PostgreSQL adds reschedule_proposal JSONB with a null default through the existing
idempotent startup schema migration. Existing requests continue without a proposal.
Proposal options use the existing options storage and request-owned cleanup paths.
At 100 total options, further proposals are rejected; no unbounded history is added.
Existing confirmation scan caps and PostgreSQL publication-parity limits still apply.

GET/detail/list expose proposal metadata only through existing principal-scoped
request reads. Notifications/audits contain no source-calendar titles. Production
session middleware controls the actor; the public demo still has demo identities.

## Calendar/UI contract

Inbox and sent views show current confirmed time separately from the pending proposal.
Only a successful server result updates the selected time. Errors ask users to refresh
because a lost response may follow a committed transaction. ICS exports the selected
time with the same stable UID; pending proposals do not change exported start/end.

Google and previously imported ICS events are NOT automatically updated or cancelled.
Users must update external calendars manually; importing again can duplicate events.
No provider-write permission, attendee invitation or cross-provider reservation is
claimed. OAuth acceptance #76 and reliable scheduled sync #89 remain separate.

Validation covers state transitions, both actors, stale/unauthorized actions, concurrent
approval, cancellation/competing-booking races, old-slot reuse, effect rollback,
deletion fences, API input handling and UI retry identity/confirmed-time updates.
PostgreSQL and Firestore emulator integration tests run with CI services.
