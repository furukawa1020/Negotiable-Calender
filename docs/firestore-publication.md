# Firestore public projection publication

Related: #88. This is the public projection batch-integrity portion, not completion
of the whole synchronization consistency issue.

## Invariant and tradeoff

Every Projection.Replace and Projection.DeleteForUser first commits a durable
users/{userID}/projectionControls/publication gate. While an operation is active
or incomplete, public reads return an empty projection list. Only a successful
replacement publishes the collection. Failed deletion never exposes leftover rows.

Each 400-operation batch checks the gate owner transactionally, alongside any
calendar sync/disconnect lease. Deletion may revoke an active replacement.
A reader checks readiness and the unique operation ID before and after its query;
a changed ID discards the result, including a ready -> busy -> ready transition.
The independent calendarDisconnected marker still prevents disconnected or
not-yet-successfully-resynchronized calendars from becoming public.

This deliberately sacrifices availability during replacement instead of keeping
a separate previous snapshot. It does not introduce per-generation collections
or a growing history: one gate document is reused for each user.

## Failure and recovery

- Validation happens before taking ownership. Replacement rows must overlap the
  requested range so every partially inserted row is covered by repair.
- A failed operation keeps the gate hidden and records the affected range.
  Best-effort cancellation cleanup releases only its own lease, never publication.
- A crashed writer's lease expires after two minutes, but data remains hidden.
- A replacement retry must cover the entire failed range. A narrower or shifted
  window is rejected with an explicit repair error instead of publishing leftovers.
- Recover by retrying a complete covering range, or by successfully calling the
  existing Projection.DeleteForUser invalidator before rebuilding the desired
  window. An interrupted full deletion must be retried before any replacement.
  Do not clear the gate manually to bypass recovery.
- In-flight stale batches and stale completion cannot mutate a successor's data.
- No recovery procedure should modify private event data or OAuth grants merely
  to repair generated public projections.

## Migration and rollback

No index or bulk data migration is required. Missing publication controls are
legacy committed collections; their first replacement creates the durable gate.
Account deletion already removes projectionControls after the generated rows.

All writers must run the new code for the invariant to hold. Before enabling
real-account traffic, drain old-revision requests/workers and ensure old Cloud Run
revisions receive no traffic. Direct administrative writes to scheduleProjections
bypass the invariant and must only occur while serving is disabled.

Do not roll back to a binary that ignores the gate while an update is incomplete.
Prefer a forward fix. If an old binary is unavoidable, disable public serving and
sync writers, complete deletion of affected generated projection collections,
then rebuild them completely before reopening traffic. Never remove only the
publication control document to make a blocked calendar visible.

## Verification

The normal Firestore emulator CI suite runs:
- complete replacements below, at and above 400 records, with concurrent readers;
- a real decode failure after a committed 400-deletion batch, hidden results and
  rejection of an insufficient repair window;
- cancellation after a committed batch, delayed writes/completion and recovery;
- expired ownership, interrupted full deletion and stale cleanup;
- legacy reads, input validation and fail-closed malformed controls.

Run with FIRESTORE_EMULATOR_HOST set, then:
`go test -race -count=1 -v ./internal/firestorestore` from apps/api.

## Scope and issue tracking

The sections below cover committed private inputs and policy/override revisions
for Firestore. #88 is tracked across these changes and their regression tests.
This protocol does not turn several writes into one physical database snapshot:
intermediate rows exist but are never accepted by the application as committed.
All writers/readers must use the protocol. Direct administrative mutations and
old application revisions bypass it.

## Policy and manual-override revision fencing

Firestore policy upserts and manual-override creation atomically write a fresh
policyRevision control ID with the setting change. Duplicate override creation
rolls back both writes. Public reads compare the completed projection's policy
revision with this control, so an old snapshot becomes hidden as soon as a
setting change commits, even if regeneration never starts or fails.

The real Rebuilder captures the revision before reading settings and overrides.
Every publication batch and completion transaction checks it again. Initial
default-policy creation recaptures the revision BEFORE reloading the policy;
concurrent user edits cannot be replaced in the published result by stale defaults.
Changes during a multi-batch publication prevent remaining batches and completion.
Abandonment may release the publication lease after a policy change, but cannot
make the incomplete collection visible.

When a new policy revision is published, all generated rows from older policy
revisions are deleted, including rows outside the requested regeneration window.
Only the regenerated window is reopened. Ordinary same-policy window replacement
retains the existing window behavior. This is intentionally fail-closed.

Missing policyRevision is the legacy empty revision. Existing projections remain
readable until a policy mutation creates a revision. A malformed revision fails
closed. Do not delete revision controls independently to bypass invalidation.
No new collection or index is required; account deletion already includes
projectionControls. Old binaries ignoring policyRevision are not safe rollback
targets after settings change; use the serving-disabled recovery procedure above.

Regression tests include a paused real Rebuilder with concurrent policy edit,
stale batches/completion after 400 writes, immediate invalidation, narrow-window
regeneration, manual overrides, duplicate-write rollback and cross-user isolation.
PostgreSQL policy revision fencing is outside this Firestore protocol.

## Committed private calendar input

users/{userID}/projectionControls/privateInputs holds a unique input ID, readiness,
and a two-minute writer lease. ApplyChanges commits an unready control BEFORE its
first data write; all data batches validate the input lease and any calendar sync
lease in the same transaction. Only the last successful operation marks it ready.
Lease expiry or best-effort cancellation cleanup never marks incomplete data ready.

Public readers require both the completed policy revision and completed private
input revision recorded in their projection gate to match the current controls.
ListPrivateEvents checks readiness/revision before AND after the collection read,
and rejects a revision mismatch instead of returning a partial or mixed list.
The real Rebuilder captures private input before loading settings/events; each
publication batch/completion rechecks it, preventing stale captured input from
being published even if another input update has already finished.

A failed input update forces the next AcquireSync to clear its stored/returned
sync token, so the existing Google sync flow requests a full response. An
incremental ApplyChanges is explicitly rejected until full recovery succeeds.
A full response replaces the whole private-event cache, not just overlapping
rows: this removes abandoned partial data when the sync window moves. Events
outside the current requested window are intentionally not retained by full sync.
Incremental success still applies cancellations and upserts to the committed cache.

New private input revisions invalidate all previous public rows, including rows
outside a smaller rebuild window. A subsequent valid regeneration removes those
obsolete generated rows before publication. An interrupted projection with no
recorded committed input revision is also fully cleared on retry with a valid
private input revision, allowing moving-window sync recovery without reopening
a partially modified older window.

Disconnect atomically revokes the private-input writer alongside its calendar
grant; only successful cleanup commits an empty input cache. The independent
calendarDisconnected marker remains closed until reconnection and successful
synchronization. A stale writer cannot recreate rows or finish over this control.

### Validation and migration

No new collection/index/history is required. One control per user is reused;
account deletion already removes projectionControls. A missing privateInputs
control is legacy data with empty input revision. After the first new-code sync,
all publication requires a completed input revision. Before activating real-user
traffic on an installation upgraded from old multi-batch writers, drain old
writers and perform a successful full sync/rebuild: legacy absence alone cannot
prove that old data was never partially written. Do not delete input controls to
unblock publication. Use full resynchronization or completed disconnect cleanup.

The emulator suite tests 3/400/405-event syncs, incremental updates/cancellations,
a real Commit RPC failure after 400 persisted rows, rejection of partial reads and
rebuilds, automatic empty-cursor selection and shifted-window full recovery,
a paused real Rebuilder across input revision changes, cancellation, overlapping
writers, expired leases and stale writes/completion after disconnect. Google data
and failures are synthetic. These are not real-account OAuth acceptance tests.
