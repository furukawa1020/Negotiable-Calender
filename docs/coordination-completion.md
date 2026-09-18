# Coordination completion (Epic #102)

The first vertical slice turns an accepted meeting option into a visible confirmed
time and an explicit calendar handoff, for both the requester and manager.
The inbox updates the accepted option ID immediately after a successful response;
persisted request data supplies the same ID after reloading.

GET /api/v1/requests/{requestId}/calendar.ics is authenticated through the existing
session middleware and restricted to the requester/target. Only an accepted,
valid meeting option exports. Missing/unauthorized requests return 404; an
unconfirmed or invalid selection returns 409. Responses are no-store.

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

- Confirmed-meeting cancellation, atomic app notification/audit and reservation
  release are implemented in [confirmed cancellation](confirmed-cancellation.md).
  [Safe rescheduling](meeting-reschedule.md) now retains the original reservation
  through proposal/decline/withdrawal and swaps it atomically on counterpart approval.
- Acceptance-time expiry/coverage validation and atomic conflict protection are
  implemented in the [confirmation engine](confirmation-engine.md). Additional
  source-sync freshness and distributed Google writeback remain separate work.
- Full negotiation end-to-end coverage, including non-meeting outcomes.
- Live Google consent and acceptance (#76), reliable scheduled sync (#89).
  The [bounded external sync trigger](scheduled-calendar-sync.md) is implemented;
  approved production identity provisioning/activation remains a separate gate.
- Optional direct Google writeback only with additional explicit consent and
  idempotent retry/cancellation semantics.

Do not close #102 based on ICS export alone. Operational deletion retries (#101)
are paused while these core milestones take priority.
