# Coordination completion (Epic #102)

The first vertical slice turns an accepted meeting option into a visible confirmed
time and an explicit calendar handoff, for both the requester and manager.
The inbox updates the accepted option ID immediately after a successful response;
persisted request data supplies the same ID after reloading.

GET /api/v1/requests/{requestId}/calendar.ics is authenticated through the existing
session middleware and restricted to the requester/target. Only an accepted,
valid meeting option exports. Missing/unauthorized requests return 404; an
unconfirmed or invalid selection returns 409. Responses are no-store.

The returned request must match the URL resource, and Firestore participant reads
must match the document ID. Mismatches return 404 without a calendar attachment.
Export uses the same immutable meeting-evidence check as confirmation: exactly
one selected candidate, owned by this request, with a valid meeting range. Missing
workspace identity and ambiguous/duplicate selected IDs fail closed with 409.
These checks do not require the meeting to still be in the future or the source
calendar to be freshly synchronized: reading a historical confirmation is not a
new booking. A pending reschedule continues to export the original confirmation;
after acceptance, export uses the new confirmed time with the same stable UID.

The ICS includes only the coordination title and confirmed time, never source
calendar details, attendees, tokens or provider identifiers. It uses a stable
hashed UID, UTC timestamps, private classification, text escaping and UTF-8-safe
75-octet folding, following [RFC 5545](https://www.rfc-editor.org/rfc/rfc5545).
There is no METHOD/ORGANIZER/ATTENDEE: this is a personal import, not an invitation.
Repeated imports may still duplicate events in clients that ignore the UID.

The user downloads the file and imports it into their calendar. This does not
write to Google, request additional OAuth scopes, email invitations, reserve a
slot, or synchronize later changes/cancellations. The UI states these limits.
Displayed times use the device timezone; exported UTC values preserve the instant.

## Remaining core milestones

Ended accepted meetings can now be explicitly marked `completed` by either
participant via `POST /api/v1/requests/{id}/complete` with `optionId`. The current
workspace/membership and deletion fences are checked even on retries. The server
requires the exact selected meeting evidence, an end time no later than server
time, and no unresolved reschedule proposal. Same-selection retries preserve the
first completion time and do not repeat effects. Firestore and PostgreSQL commit
state, counterpart notification and audit together; any failure rolls back all.

The UI updates cancellation/completion actions at meeting start/end and on tab
resume, without API polling. Confirmation is bound to the exact displayed meeting
and is reset after selection, time, status or proposal changes. The server remains
authoritative for time and permissions. Completion preserves the selected option and
historical ICS export; it removes the request from accepted reservation scans.
It records a participant's action, not independently verified attendance. No
background completion/backfill, external calendar change or added scope occurs.
The client validates the response's request and selected option, uses the existing
20-second command deadline, and ignores responses after account/workspace changes.

- Confirmed-meeting cancellation, atomic app notification/audit and reservation
  release are implemented in [confirmed cancellation](confirmed-cancellation.md).
  [Safe rescheduling](meeting-reschedule.md) now retains the original reservation
  through proposal/decline/withdrawal and swaps it atomically on counterpart approval.
- Acceptance-time expiry/coverage validation and atomic conflict protection are
  implemented in the [confirmation engine](confirmation-engine.md). Additional
  [Source-sync freshness](source-freshness.md) now gates calendar-backed snapshots;
  distributed Google writeback remains separate work.
- Full negotiation end-to-end coverage, including non-meeting outcomes.
- Live Google consent and acceptance (#76), reliable scheduled sync (#89).
  The [bounded external sync trigger](scheduled-calendar-sync.md) is implemented;
  approved production activation and authenticated trigger verification are recorded
  in that runbook. Live Google OAuth synchronization remains unverified (#76).
- Optional direct Google writeback only with additional explicit consent and
  idempotent retry/cancellation semantics.

Do not close #102 based on ICS export alone. Operational deletion retries (#101)
are paused while these core milestones take priority.
