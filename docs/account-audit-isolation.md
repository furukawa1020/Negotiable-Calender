# Actor audit cleanup isolation (#153, related #94/#101)

The actor-specific phase of Firestore account deletion previously read and
decoded every audit in each recorded organization, then compared ActorUserID.
An unrelated record with malformed event metadata could block deletion, and
read work grew with all organization audit history.

The phase now uses collection-scoped `ActorUserID == userID` and an empty
`Select()` (document references only). It does not decode event bodies. Matching
actor records are removed even when other fields are malformed. Nonmatching or
missing-actor records are not read/deleted by this phase; other organizations are
not queried. Request-related audit cleanup, retry order, writer fences and owner
checks are unchanged. Missing-actor/orphan remediation remains separate work.

The emulator regression includes 405 unrelated malformed records, missing actor,
matching records with malformed metadata, a foreign organization, retained shared
organization ownership and idempotent retry. Existing failure-injection tests
still verify recovery after partial actor deletion and request-specific cleanup.

No schema/backfill or scheduled cleanup is introduced. The query uses the normal
single-field ActorUserID index. If that index is disabled, fail closed; never fall
back to an unbounded audit scan. The emulator is not evidence of production index
configuration. A read-only field describe on 2026-09-25 confirmed the production
auditLogs/ActorUserID collection-scoped ascending index is READY and inherited
from the default field configuration. No index settings were changed, and no
real-user deletion is performed by this change.

This reduces actor audit reads, not every scan in account deletion. Request and
other cleanup costs still depend on existing data. No new retention promise or
removal of deletion lifecycle markers is made. Prefer a forward fix; rolling back
restores the unrelated-record failure mode.

API reference: https://pkg.go.dev/cloud.google.com/go/firestore#Query.Select
