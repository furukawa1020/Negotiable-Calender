# Scheduled sync index incident (#155, related #89)

On 2026-09-25, scheduled runs including 36073977695 failed with HTTP 503 after
caller authentication succeeded. Aggregate logs showed `claim_failed`, zero
claimed/attempted connections. The production default database's composite index
list was empty. A read-only equivalent due query, limited to one document with
ID-only projection, returned FAILED_PRECONDITION and the missing-index error.
No returned IDs, authorization tokens or raw error URLs were printed.

The required calendarConnections index is already declared in
`firestore.indexes.json`: ReconnectRequired ASC, NextAttemptAt ASC, collection
scope. Restoring this exact previously approved index does not require changing
OAuth grants, IAM roles, billing plans or stored calendar records. Wait for READY
before verification. The cause of its disappearance has not been established;
do not attribute it to an operator or a deployment without audit evidence.

The worker now logs an allowlisted `cause_code` alongside `claim_failed`.
Wrapped gRPC status and context failures are categorized; unrecognized errors
remain `unknown`. Failed precondition is NOT automatically labeled missing index.
Never log the raw error, provider text, index-creation URL, token or account ID.

## Recovery checks

1. Read the explicit project's index configuration; compare fields, collection
   scope and READY state with the checked-in definition. Do not delete other
   indexes or fall back to scanning all calendar connections.
2. Run the existing approved authenticated bounded workflow once. Zero claimed
   connections is valid for scheduler health, not proof of real Google sync.
3. Observe a timer-triggered run separately; a manual dispatch alone does not
   establish timer execution. Log run IDs/outcomes in #155/#89.
4. Keep anonymous/forged requests rejected and retain all work/time bounds.

No periodic cleanup, index-deletion operation, or cloud IAM grant is introduced
by this code change. Real consent and synchronization acceptance remain #76/#121.
Emulator tests cannot detect missing production composite indexes, so do not use
their success as a substitute for the production index check.

## Startup guard (#157)

Firestore startup runs a read-only due-query preflight before seeding or serving
HTTP when sync mode is `external` or `background`. Both legacy-null and dated
queries share their predicates/order with actual claims. Each query has an
ID-only projection and limit 1, with one five-second total deadline (or the
earlier startup deadline). Empty results pass; no connection bodies, tokens,
leases, transactions or Google calls are involved. PostgreSQL and `off` mode
are unchanged. This uses existing runtime data-read access, not index-admin IAM.

Failure exits before the revision can become ready and logs a fixed message,
never a raw provider error or index-creation URL. The deployment's startup probe
and smoke check therefore cannot mark a newly started revision healthy while
these required query shapes fail. Do not delete a production index to test this.
CI verifies bounds/read-only behavior using an isolated emulator and injects
query failures; a successful production rollout is the real-index acceptance.

This is startup-only, not continuous drift monitoring. An index removed after
startup is still detected by the scheduled worker, not by `/health` or `/ready`.
It does not prove Google authorization, token freshness, event synchronization,
or correctness of every stored connection. Cold starts add two bounded queries;
Firestore minimum-query/index-read billing still applies. A result limit is not
a strict index-entry scan cost cap, nor is this a promise of zero cost.

Reference: [Firestore index model and serving state](https://docs.cloud.google.com/firestore/docs/reference/rest/Shared.Types).
