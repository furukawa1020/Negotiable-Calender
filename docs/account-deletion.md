# Firestore account deletion

Related: #94 (deletion lifecycle), #85 (last OWNER), #88 (publication integrity).

## Start and writer fencing

Deletion begins with a Firestore transaction that reads the actual organization
memberships, not the user's workspace cache. A departing OWNER with remaining
members requires another OWNER who has not started deletion. Concurrent deletions
read each other's lifecycle markers, so they cannot both count the other as a
valid successor. Rejected authorization leaves the marker and calendar grant
unchanged. This implementation scans organizations in the transaction; large
installations should monitor its read cost and transaction limits.

An authorized start writes accountDeletions/{userID} with phase=deleting and the
organization IDs needed for retry, and deletes the calendar connection atomically.
The marker is outside users/{userID}, so deleting user subcollections cannot remove
the fence. Public projection reads become empty immediately; private-input reads
and rebuilds reject the account. Every fenced sync/publication batch, sync acquire,
calendar OAuth flow/save, policy update, notification, session creation, workspace
creation/switch and invitation acceptance checks the marker transactionally.
Request creation and mutation also check every participant, including option
delegates before and after mutation. Request audit writes read the request and
all its participant markers in the same transaction, even when the caller
supplies an organization ID. A mismatched organization is rejected.

Existing sessions are rejected while deletion is pending. A callback already
exchanging a Google token still cannot save to the old user ID. Completion deletes
the user document and replaces the lifecycle record with phase=complete in the
same transaction. The minimal completed marker retains the old user ID (document
key), start time and phase, not profile data, OAuth grants or memberships. These
are pseudonymous lifecycle records; this is not a claim of complete data erasure
or regulatory compliance. No automatic tombstone expiry is configured: removing
them without a replacement stale-writer strategy is unsafe.

## Interrupted cleanup

The deletion stays pending and fail-closed on error; it is never rolled back to an
active account. Cleanup can be retried using the persisted organization IDs, even
if the workspace cache has already been removed. Successful completion is
idempotent. Because the old session is invalidated, retries currently require an
authorized operator; there is no unauthenticated public retry endpoint.

From apps/api, with application-default credentials authorized for the explicit
Firestore project, an operator can run:

```text
go run ./cmd/account-cleanup -project PROJECT_ID -user PENDING_USER_ID
```

The single-user command only resumes an existing deleting marker (or confirms
complete). It cannot authorize or initiate a new deletion. It has a five-minute
default timeout and never accepts a collection/path as its target.
Do not run it against real accounts without checking the exact authorized target.
The implementation/tests do not execute production account cleanup.

### Bounded pending sweep (#101)

An explicitly requested `-pending` mode can retry one page of existing `deleting`
markers. It is mutually exclusive with `-user`. First inspect a bounded dry run:

```text
go run ./cmd/account-cleanup -project PROJECT_ID -pending -dry-run -limit 25
go run ./cmd/account-cleanup -project PROJECT_ID -pending -limit 25 -timeout 5m -account-timeout 30s -cursor-out NEW_PRIVATE_CURSOR_FILE
go run ./cmd/account-cleanup -project PROJECT_ID -pending -limit 25 -cursor-in PREVIOUS_PRIVATE_CURSOR_FILE -cursor-out ANOTHER_NEW_PRIVATE_CURSOR_FILE
```

These are operator examples, not a production execution instruction. Verify the
project, pending deletion authority, cost and protected output location before
running. No scheduler, IAM role, production batch or new deletion is enabled by
this change. Ordinary API operation does not invoke this CLI.

- Selection is `Phase == deleting`, ordered by document ID, limited to 1–100
  markers plus one lookahead (default 25). It does not enumerate active users or
  completed markers. Selection failure does not fall back to an unbounded scan.
- Each target rechecks its existing marker via `ResumeAccountDeletion`; complete
  is idempotent, missing/invalid states cannot start deletion. Corrupt markers
  count as failures without preventing later selected accounts from running.
- Total context deadline includes client initialization, page read and cleanup:
  default 5m, maximum 15m. Per-account default/maximum is 5m and can be shortened;
  the remaining total deadline always wins. Pending requests are cancelled when
  the context expires. This is not a guarantee of instantaneous rollback of an
  in-flight server commit. Existing deletion fences and retries remain necessary.
- Stdout is a count-only JSON summary; stderr has fixed errors, no IDs, paths,
  credentials or provider responses. Any failure/interruption exits nonzero.
  Dry run only reads the pending page; it does not certify cleanup will succeed.
- Cursors encode a pseudonymous user ID: encoding is NOT encryption/anonymization.
  They are written only to an explicitly named new file (`O_EXCL`, mode 0600),
  never stdout/stderr, never overwrite files. Windows operators must protect the
  parent directory with suitable ACLs; Unix mode bits alone do not secure Windows
  files. Keep cursor files out of logs, Git and public artifacts. Use only within
  the same project. Cursor state is not an authorization credential.
- Check the exit status before using a cursor. Initialization/output failure can
  leave an empty/partial file; restart from the beginning instead of trusting it.
  Empty cursor means start a new sweep. When `hasMore` is true the cursor advances
  past attempted accounts, including failures, so other accounts can make progress.
  At sweep end start again without a cursor to revisit failures and newly added
  IDs behind the cursor. The page is not a database-wide snapshot or lock.
- On a total timeout, `remaining` counts selected but unattempted accounts, and
  the cursor points after the last attempted account (or the incoming cursor).
  Without `-cursor-out`, restart from the beginning; completed markers are excluded.
- Per-account cleanup still scans related request/organization data and may use
  multiple batches. This limits targets and time, NOT a strict total read/write
  quota or cost. It does not fix historical orphan data, revoke a lost Google
  token, remove deletion tombstones, or establish a new retention deadline.

Unit tests cover deadlines, failure continuation, mode validation, cursor files
and redaction. Firestore emulator tests cover 105 markers, stable pagination after
completion, corrupt markers, no active-account changes and actual retry cleanup.

## Request, notification and audit cleanup

Request cleanup first deletes notifications from every participant's inbox,
then request-specific audit events in the request's own organization, then the
request. Until both dependent sweeps succeed,
the request itself is the durable retry reference; no growing ID array or
additional retained ledger is needed. Actor-specific audit cleanup follows.
Failures during either sweep or the request delete can be replayed safely.
Other resource types and other organizations are not selected by request ID.
The write fences prevent delayed handlers from recreating these audit events.
This does not recover orphan audit events already left by an older binary.

Notification creation transactionally requires an existing request, a recipient
who is its requester, target, current delegate or option delegate, and active
account markers for every participant. Deleting any participant therefore stops
delayed notification writes even to a different, active user's inbox.

Cleanup deduplicates those recipients (including historical option delegates)
and runs one collection-scoped RequestID equality query per recipient. It does
not scan all users or use a collection-group index; it relies on the default
single-field RequestID index. Read/delete cost grows with matching notifications
and distinct participants, not unrelated inbox contents. Parent user existence
is not required to clean a participant's notification subcollection.
See the [Firestore single-field index documentation](https://firebase.google.com/docs/firestore/query-data/index-overview).
The production notifications/RequestID collection-scoped index was confirmed
READY with a read-only field describe on 2026-09-14; no index settings were changed.

Notifications for other requests remain untouched. Legacy notifications in
nonparticipant inboxes or referencing already missing requests require separate
orphan remediation; this change does not perform a production-wide purge.

## New sign-in and migration

Sign-in is denied during pending deletion. After completion, a new Google sign-in
may create a fresh user ID/workspace; it cannot reactivate the old ID. Identity
updates compare the prior identity mapping transactionally so racing sign-ins do
not each create an orphan new account. A competing sign-in may need to retry.

No bulk migration or index is required. No marker means the account is active.
Old binaries bypass deletion fencing: drain old revisions before enabling this
workflow and prefer forward fixes. Do not roll back to a binary that ignores the
markers while deletions are pending, and do not remove markers to unblock users.

## Verification and remaining work

Required emulator CI covers stale sync/OAuth/session writes, 405-row cleanup,
failure after the first 200-row delete batch, hidden publication during failure,
retry, stale/missing workspace caches, simultaneous OWNER deletions, and fresh
sign-in after completed deletion. All fixtures are synthetic.
Request/audit tests additionally inject failures after a committed audit delete,
at request deletion, and during the later actor audit sweep; they verify retry
completion, organization/resource-type isolation, participant fencing and a
mutation that attempts to introduce a deleting delegate.
Notification regressions cover all four participant roles in pending/completed
deletion, unrelated/missing requests, partial notification deletion and replay,
historical delegates without parent user documents, and unrelated inbox entries.

Request/audit cleanup remains multi-pass but preserves its retry references.
Background retries, pre-existing orphan audit remediation,
and lifecycle-record retention need additional review. Keep #94/#88 open for these
cross-path checks. PostgreSQL deletion concurrency is not changed by this
Firestore-specific implementation; do not infer its safety from these tests.
