# Source freshness for calendar-backed availability (#115)

Calendar data has a private source receipt: unique revision, provider observation
time and confirmed coverage interval. Observation time is captured before the event
read, not when policy projections are regenerated. No event title or credential is
included. The receipt is not returned in public availability responses.

## Decision rule

- Calendar-backed availability is usable only while observation age is strictly
  less than 30 minutes, the observation is not in the future, and coverage includes
  the entire proposed meeting. Exactly 30 minutes is stale.
- A receipt must match both the published input revision and the committed sync
  timestamp. Unknown receipts, reconnect-required state, any recorded sync error,
  and any outstanding sync lease (even expired) hide the imported availability.
- Projection expiry is capped at observation time + 30 minutes. A policy edit or
  manual projection rebuild cannot renew that deadline or expand source coverage.
- Candidate reads filter unavailable source snapshots. Candidate generation also
  rejects expired/future projections and unknown availability. With no proven slot,
  existing asynchronous alternatives remain available.
- Acceptance and reschedule acceptance recheck the same source within the booking
  transaction. The existing safe `availability_changed` response advises refresh/
  sync and proposing another time. A refused reschedule keeps the original booking.

The 15-minute external trigger is best effort, not an SLA. Delayed or failed sync
can therefore close calendar-backed availability. The owner can retry manual sync
or reconnect when required; only a completed fresh snapshot reopens it. The UI
shows last successful sync and a 30-minute freshness warning. It does not promise
that Google has had no changes since the snapshot.

## Storage and commit ordering

PostgreSQL stores the receipt and published revision in `calendar_source_snapshots`
(additive startup migration). Private event changes and the receipt commit together.
Rebuild captures a source generation before reading events; publication holds the
existing per-calendar advisory lock, rechecks that generation and updates rows plus
published revision atomically. A source generation change clears all old projection
rows, not just the requested rebuild window. Public reads use a repeatable-read
snapshot; booking transactions use the existing serializable isolation and calendar
lock. PostgreSQL policy-revision parity is separate from this source-generation fence.

Firestore retains the receipt in the existing private-input control. It stays
unreadable during multi-batch writes; input, policy and publication revisions fence
each write and completion. Public reads check publication before and after reading
rows. Booking transactions read the connection, source and publication controls
together. Existing disconnect and account-deletion fences remain active.

In both stores the public projection is built before sync completion, but is not
readable until the sync timestamp commits and the sync lease is released. A failure
between those operations cannot make the new generation public. A subsequent sync
with an uncommitted/unknown base performs a full fetch before publishing again.

## Coverage, migration and modes

A delta sync retains the coverage of its full-sync base; moving the requested window
does not prove newly added periods. If the base is unknown/uncommitted or no longer
covers half of the configured past/future lookaround, the sync handler drops the
cursor for that fetch and rebases with a full response. A 410 cursor expiry retains
the existing full-fetch recovery. Until rebasing, periods outside the proven window
are unavailable, even if old projection rows exist there.

Existing connected accounts without receipts fail closed until a successful full
sync. No synthetic migration timestamp makes legacy data fresh. Deploy all reader,
writer and confirmation changes together and drain old revisions before relying on
the guarantee. There are no new OAuth scopes, scheduler permissions or external
calendar writes.

Never-connected policy-only availability and seeded demo data remain supported;
they are not evidence of Google availability. Connecting immediately applies the
external-source gate. Disconnect does not silently revert to policy-only: Firestore
retains its existing disconnect marker and PostgreSQL retains an empty receipt
tombstone. Account deletion removes these controls (PostgreSQL FK cascade / existing
Firestore control cleanup). Cancellation of already-confirmed meetings remains
possible without a fresh source; accepted meetings are not automatically cancelled.

The target's imported availability is covered here. This is not a reservation in
Google, an instantaneous external change detector, or a new check of the requester's
private Google events. Real-account OAuth acceptance remains #76.

## Verification

Unit tests use a fixed clock for age/coverage boundaries, future observations,
unknown bases, policy rebuild expiry caps and delta/full-fetch selection. Both DB
integration suites exercise stale/unknown/future sources, failure, pending commit,
active lease, reconnect, disconnect, uncovered periods, a paused old generation and
reschedule rejection retaining the original reservation. Existing fake-provider
Firestore OAuth/sync tests cover actual full/delta publication and recovery without
live Google credentials. Failure injection is confined to test databases/emulators.
