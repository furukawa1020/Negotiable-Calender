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
| Target projections | Target subcollection, `EndAt > earliestCandidateStart` | 257 documents (256 + overflow sentinel) |
| Accepted bookings | `TargetUserID == participant` AND `Status == accepted` | 65 per participant |
| Accepted bookings | `RequesterUserID == participant` AND `Status == accepted` | 65 per participant |

There are two participants and at most five queries: **517 query document results maximum**, including overflow sentinels. Projection source/publication/deletion controls are checked before and after (at most 18 point reads); both participants' user documents and deletion markers are checked before and after (8 point reads). Thus the source loader issues at most **543 logical document reads**, plus the handler's request and membership reads (2) for **545 before authentication overhead**. Tests intercept actual SDK `RunQuery` and `BatchGetDocuments` requests to verify the query limits and point-read counts. SDK/service retries and billing rules are not covered by this logical-operation bound, and this is not a yen-denominated cap.

Overflow returns no partial data and a sanitized `503`. Rows outside the requested upper time bound can conservatively exhaust the projection budget; accepted history is not date-filtered, so a long accepted history can exhaust a role's budget. This is deliberate: correctness wins over a falsely clear calendar. A future complete, versioned interval read model can reduce conservative denials. Do not add client-side pagination to bypass these caps.

Booking queries deliberately span organizations and include malformed accepted records. Stale/contradictory projection rows are not filtered out as if they were free time. Existing validators reject stale coverage, gaps and corrupt/conflicting accepted records. The final projection publication/source check and account guards reject a deletion, dirty publication, or disconnect detected during loading. These checks are not an atomic reservation; changes after the reads still require transactional final approval.

## Indexes and rollout

The projection query has one range field (`EndAt`); booking queries have only two equality fields (`TargetUserID`/`RequesterUserID` and `Status`). They use default automatic single-field indexes and equality index merging; no composite-index deployment or collection scan fallback is required. If a project exempts these fields or query planning fails, the preview stays unavailable rather than scanning.

- [Firestore index merging](https://firebase.google.com/docs/firestore/query-data/index-overview)
- [Firestore billing and single-range query rules](https://firebase.google.com/docs/firestore/pricing)

The emulator does not prove production index configuration. Verify the three query shapes with production Query Explain (plan-only, synthetic nonmatching user ID) during rollout. Do not log credentials or actual calendar records. Existing index exemptions must not be silently removed.

Production plan-only verification on 2026-09-19 succeeded for all three shapes: `EndAt ASC` for projections, and `Status ASC` merged with `TargetUserID ASC` / `RequesterUserID ASC` for bookings (each with the implicit document-name index suffix). No composite indexes were added. Query Explain default mode bills a minimal read even though it does not execute the data query; this verification is not a zero-operation claim.

## Verification

Go tests cover concurrent admission, window expiry, bounded limiter memory, session/workspace rotation, `429` before source reads, and unsupported-store failure. Firestore emulator tests use large closed histories and historical projections, exact limit/overflow boundaries, cross-workspace accepted conflicts, corrupt accepted records, stale projections, and account/publication changes between queries and final recheck. gRPC interception verifies actual server-side query limits and read result counts. Web tests cover sanitized retry delays and normal-flow fallback. CI runs emulator tests with the race detector; ordinary local Go runs without `FIRESTORE_EMULATOR_HOST` skip emulator cases and must not be reported as live-storage verification.
