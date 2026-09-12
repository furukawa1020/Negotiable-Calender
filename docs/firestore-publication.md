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

## Remaining work in #88

The privateEvents input collection still uses multi-batch changes without a
committed-input snapshot. A concurrent policy-triggered rebuild can therefore
read incomplete synchronization inputs. Policy/manual-override revision fencing is now implemented for Firestore as
specified below. It does not make private-event inputs atomic. The projection
gate alone does not prove these end-to-end synchronization properties. Keep #88 open until those
paths, automatic recovery for moving synchronization windows, and their failure
tests are implemented.

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
This does not implement PostgreSQL policy revision fencing or committed snapshots
for Firestore privateEvents; those remain separate work.
