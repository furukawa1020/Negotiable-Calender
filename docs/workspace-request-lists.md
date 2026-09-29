# Workspace-scoped request lists

`GET /api/v1/requests` (`scope=inbox` or omitted) and `scope=sent` list only
the authenticated actor's received/sent requests in their active workspace.
Production authentication derives actor and organization from the session and
overwrites the demo identity headers; client-provided headers are not authority.

## Read boundary

- Missing actor or active organization: 401; invalid scope: 400.
- Current membership is required even when old requests still reference the actor.
  Revoked/nonmember access returns 403, not historical request data.
- PostgreSQL reads membership, request envelopes and options in one repeatable-read
  read-only transaction. Firestore reads the deletion fence, membership and scoped
  query in one read-only transaction.
- A participant can still see their history when the *other* participant has left.
  Membership alone does not grant access to another participant's requests.
- Both stores retain descending creation-time/ID ordering and normalize empty
  options. Empty lists serialize as arrays. Decode/storage errors return no partial
  list. The API also rejects out-of-workspace or wrong-role store results.
- Stores without the scoped capability fail closed (503); no global-read fallback.
- Workspace changes discard old UI rows and in-flight reads; new reads carry the
  selected workspace. A failed new-workspace read cannot restore old rows.

Firestore uses two equality filters (organization plus requester/target), without
server-side ordering. This can use single-field index merging; see the
[official index documentation](https://firebase.google.com/docs/firestore/query-data/index-overview#index_merging).
No schema or index migration is introduced. Emulator coverage does not prove
production index availability; a release check may run the same query with a
fresh synthetic non-existent organization/user ID and must not read real records.

## Deliberately unchanged

The internal `ListForUser`/role-specific global methods remain for cross-workspace
booking-conflict detection and personal data export. Request detail/ICS endpoints
retain their existing participant policy; this change does not claim to make all
history endpoints workspace-scoped. Pagination and real Google OAuth acceptance
are separate work.

## Regression coverage

- HTTP routing/identity/membership/capability failures and store contract violations.
- PostgreSQL integration and Firestore emulator: both roles in two organizations,
  outsider membership, revoked actor, departed counterpart, ordering/options, and
  unchanged global reads. Firestore additionally checks account deletion and bad data.
- UI: inbox/sent workspace headers, clearing old rows on switching, empty results
  and forbidden responses after the switch; existing stale-response tests remain.
