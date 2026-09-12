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

The command only resumes an existing deleting marker (or confirms complete). It
cannot authorize or initiate a new deletion. It has a five-minute default timeout,
does not enumerate accounts, and never accepts a collection/path as its target.
Do not run it against real accounts without checking the exact authorized target.
The implementation/tests do not execute production account cleanup.

## Request and audit cleanup

Request cleanup first deletes request-specific audit events in the request's
own organization, then deletes the request. Until that audit sweep succeeds,
the request itself is the durable retry reference; no growing ID array or
additional retained ledger is needed. Actor-specific audit cleanup follows.
Failures during either sweep or the request delete can be replayed safely.
Other resource types and other organizations are not selected by request ID.
The write fences prevent delayed handlers from recreating these audit events.
This does not recover orphan audit events already left by an older binary.

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

Request/audit cleanup remains multi-pass but preserves its retry references.
Background retries, pre-existing orphan audit remediation,
and lifecycle-record retention need additional review. Keep #94/#88 open for these
cross-path checks. PostgreSQL deletion concurrency is not changed by this
Firestore-specific implementation; do not infer its safety from these tests.
