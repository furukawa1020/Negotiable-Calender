# Bounded planning reads (#131)

This protects `GET /api/v1/requests/{id}/planning-preview`, not all application endpoints or total project spend. No paid AI service, billing setting, IAM permission, Firestore write, or new retained data is introduced.

## Admission

The authenticated identity supplied by the session middleware is the quota key; changing cookies, IP, request ID or active workspace does not reset it. Each API process admits 12 attempts per account in a one-minute window (at most four successful three-read AI comparisons). The next attempt receives `429` and a bounded `Retry-After`; the UI shows the wait without automatic retries. Unauthenticated calls receive `401`.

Quota checks precede request/membership/source reads in this handler. Authentication middleware still performs its own reads before this check. Buckets are mutex-protected, hashed, bounded to 4,096 accounts, and expired slots reclaimed when capacity is needed. New keys fail closed at capacity.

**This limiter is process-local, not a durable/distributed spend cap.** Restarts reset it; multiple instances/revisions have separate buckets (including rollout overlap). Cloud Run currently targets max one instance, but that is not a universal rate/billing guarantee. Existing Cloud Run/Firestore usage can still incur charges. Distributed quotas and whole-service cost controls remain separate work.

## Source read contract

Only a store implementing `LoadPlanningSources` can serve the preview. Firestore production implements it; PostgreSQL/custom stores currently return `503` for this optional AI preview, with no unbounded fallback. Ordinary scheduling and transactional approval are unchanged. Other backends need a bounded adapter before enabling this preview.

The handler has a 10-second context deadline. There are no application retries, counts, offsets or pagination loops.

| Read | Server query | Limit |
| --- | --- | --- |
| Target projections | Target subcollection, `StartAt < latestCandidateEnd` AND `EndAt > earliestCandidateStart` | 257 documents (256 + overflow sentinel) |
| Accepted bookings | `TargetUserID == participant` AND `Status == accepted` | 65 per participant |
| Accepted bookings | `RequesterUserID == participant` AND `Status == accepted` | 65 per participant |

There are two participants and at most five queries: **517 query document results maximum**, including overflow sentinels. Projection source/publication/deletion controls are checked before and after (at most 18 point reads); both participants' user documents and deletion markers are checked before and after (8 point reads). Thus the source loader issues at most **543 logical document reads**, plus the handler's request and membership reads (2) for **545 before authentication overhead**. Tests intercept actual SDK `RunQuery` and `BatchGetDocuments` requests to verify the query limits and point-read counts. SDK/service retries and billing rules are not covered by this logical-operation bound, and this is not a yen-denominated cap.

Overflow returns no partial data and a sanitized `503`. Only overlapping projections consume the result budget; unrelated past and future rows do not. A requested interval containing more than 256 overlapping rows still exceeds the budget. Accepted history is not date-filtered, so a long accepted history can exhaust a role's budget. Do not add client-side pagination to bypass these caps.

Booking queries deliberately span organizations and include malformed accepted records. Stale/contradictory projection rows are not filtered out as if they were free time. Existing validators reject stale coverage, gaps and corrupt/conflicting accepted records. The final projection publication/source check and account guards reject a deletion, dirty publication, or disconnect detected during loading. These checks are not an atomic reservation; changes after the reads still require transactional final approval.

## Indexes and rollout

The projection query requires the checked-in `scheduleProjections` COLLECTION composite index: `StartAt ASC`, then `EndAt ASC`. Confirmation and rescheduling use the same overlap query inside their existing transactions, retaining their 10,000-row overflow guard. A normal 120-day synchronization produces at least 11,520 quarter-hour rows; unrelated rows no longer prevent a short meeting from being confirmed.

Before deploying, add this exact index from `firestore.indexes.json` to the explicit production project/database and wait for `READY`. Preserve the existing `calendarConnections` index and any unrelated indexes. Firestore startup runs the shared overlap query with a five-second deadline, one-result limit, document-name-only selection, and a synthetic user namespace. Query failure prevents the new process from serving traffic. This startup check is not continuous drift monitoring.

Booking queries still use equality index merging for participant and status. There is no collection-scan fallback on query failure. Two-range queries can incur index-entry reads; the document-result limits above do not cap index scans or guarantee free operation.

- [Firestore index merging](https://firebase.google.com/docs/firestore/query-data/index-overview)
- [Firestore multiple-range queries](https://firebase.google.com/docs/firestore/query-data/multiple-range-fields)
- [Firestore billing and index-entry reads](https://firebase.google.com/docs/firestore/pricing)

The emulator does not prove production index configuration. Verify index readiness and the production overlap query against the synthetic nonmatching namespace before release. Do not log credentials or actual calendar records. Existing index exemptions must not be silently removed. Diagnostic queries and startup probes are not zero-operation or zero-cost guarantees.

## Verification

Go tests cover concurrent admission, window expiry, bounded limiter memory, session/workspace rotation, `429` before source reads, and unsupported-store failure. Firestore emulator tests use a 120-day publication, long spanning blockers, adjacent boundaries, large closed histories, exact limit/overflow boundaries, cross-workspace accepted conflicts, corrupt accepted records, stale projections, and account/publication changes between queries and final recheck. gRPC interception verifies that a 30-minute meeting reads only its two quarter-hour projections, plus the separate request/conflict reads. Startup tests verify the query shape, bounded field selection, cancellation and error propagation. Web tests cover sanitized retry delays and normal-flow fallback. CI runs emulator tests with the race detector; ordinary local Go runs without `FIRESTORE_EMULATOR_HOST` skip emulator cases and must not be reported as live-storage verification.
