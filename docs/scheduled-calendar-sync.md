# Request-scoped scheduled calendar sync (#89)

Cloud Run stays at min=0/max=1 with request-based CPU. External requests execute a
bounded batch synchronously; nothing continues after the HTTP response. Cloud Run
does not guarantee CPU for idle background threads ([Google guidance](https://docs.cloud.google.com/run/docs/tips/general)).

## Modes and activation status

- `CALENDAR_SYNC_MODE=background`: local ticker; Compose explicitly selects this.
- `off`: no ticker, internal endpoint disabled (404); manual user sync still works.
- `external`: authenticated POST `/internal/calendar/sync-due`; no ticker.

Unset mode defaults to off on Cloud Run (`K_SERVICE` present), background locally.
External mode requires an HTTPS endpoint audience, the dedicated service account's
numeric subject, and its email. Invalid configuration prevents startup.

Production activation is gated by repository variable `CALENDAR_SYNC_ENABLED=true`
and `CALENDAR_SYNC_SUBJECT`. Without activation, deploy explicitly sets off and the
scheduled workflow is skipped. **IAM provisioning requires operator approval and
has not been performed as part of the initial implementation.** #89 remains open
until identity/index provisioning and a real authenticated trigger are verified.
Real Google OAuth/consent is separately tracked in #76; do not claim it was tested.

## Authentication and least privilege

The caller is `negotiable-calendar-sync@improve-production-management.iam.gserviceaccount.com`,
not the deployer or runtime identity. It needs no project, Firestore, Cloud Run admin
or calendar data role. The existing WIF provider restricts the repository to
`furukawa1020/Negotiable-Calender` on `refs/heads/main`. Only that repository's
principalSet receives `roles/iam.workloadIdentityUser` on this dedicated account.
Anyone who can alter trusted main workflows can consequently invoke a sync batch.
No downloaded key or static bearer secret is created.

The handler verifies Google signature, audience, issuer, expiry, issued-at, exact
numeric subject, exact service-account email and verified-email claim. Cookies,
demo headers, normal users and unsigned/forged tokens confer no authority. No body
or query parameter can select a user or raise limits. The token is never logged.
The response is no-store and contains only aggregate counts. One process permits
one external batch at a time; persisted per-user claims/leases fence duplicates
across processes and deployments. Signature tests use a synthetic RSA key/JWKS,
not a live credential or an unverified JWT decoder.

## Work bounds and failure recovery

- One scheduled invocation, no HTTP retries: max 5 claims, 40-second batch context,
  35-second per-user context, 2-minute claim/ownership leases.
- The 5-second JWT validation budget and up to 2 seconds to record a timed-out
  sync's failure fit inside the caller's 55-second and server's 60-second limits.
- Context-aware provider/storage operations stop at deadlines. Unprocessed claims
  become eligible after their reservation delay; a later invocation resumes them.
- Existing incremental cursor recovery, backoff, reconnect stop, disconnect and
  deletion fences remain active. Failure cleanup retains the sync lease, so it
  cannot overwrite a reconnected/deleted account or a newer sync.
- Claims and writes are bounded. Firestore reads at most 25 candidate documents
  per configured batch, plus transactional account/connection reads and the actual
  event/projection work. It does not scan all connections. This is not a bound on
  total event processing reads/writes or an absolute bill cap.
- A full batch reports capacityReached (possible backlog). Failure/budget exhaustion
  fails the workflow; reconnect-required connections stop being selected. An
  unconfigured provider returns not_configured without claiming work and creates a
  visible workflow warning, not a fabricated successful calendar sync.

Firestore needs the composite index declared in `firestore.indexes.json`:
calendarConnections(ReconnectRequired ASC, NextAttemptAt ASC). Explicit-null legacy
deadlines are included with a separate bounded query; documents missing the schedule
fields require reconnect/manual repair. Missing indexes fail closed, not fallback
to an unlimited scan. Existing SaveConnection always initializes those fields.

## Approved provisioning/runbook

Only after explicit approval, use an operator identity to create the account and
grant the narrowly scoped federation binding (do not grant project-level roles):

```powershell
gcloud iam service-accounts create negotiable-calendar-sync --project=improve-production-management --display-name='Negotiable Calendar scheduled sync caller'
gcloud iam service-accounts add-iam-policy-binding negotiable-calendar-sync@improve-production-management.iam.gserviceaccount.com --project=improve-production-management --role=roles/iam.workloadIdentityUser --member='principalSet://iam.googleapis.com/projects/480760664246/locations/global/workloadIdentityPools/github-actions/attribute.repository/furukawa1020/Negotiable-Calender'
gcloud firestore indexes composite create --project=improve-production-management --collection-group=calendarConnections --query-scope=COLLECTION --field-config=field-path=ReconnectRequired,order=ascending --field-config=field-path=NextAttemptAt,order=ascending
gcloud iam service-accounts describe negotiable-calendar-sync@improve-production-management.iam.gserviceaccount.com --project=improve-production-management --format='value(uniqueId)'
```

Verify the provider condition and account policy, wait for index state READY, then
set repository variable CALENDAR_SYNC_SUBJECT to that numeric uniqueId. Set
CALENDAR_SYNC_ENABLED=true and run Deploy production on main. After deployment,
run Scheduled calendar sync via workflow_dispatch and inspect its summary/logs.
Do not count not_configured as end-to-end Google sync. Inspect a scheduled run too.
To stop: set CALENDAR_SYNC_ENABLED=false and redeploy (off). Disable the workflow
immediately if needed; until redeploy the trusted endpoint remains callable.
No account or stored calendar data must be deleted to stop automation.

## Schedule, cost and observation

GitHub runs the workflow at UTC minutes 7,22,37,52. This is best effort: runs can be
delayed/dropped, and public-repository schedules disable after 60 days without
activity ([GitHub schedule limitations](https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows)).
There is no 15-minute SLA. Monitor workflow failures and inactivity; an external
heartbeat alert with an independent scheduler is future operational work.

This public repository uses a standard Ubuntu hosted runner, which is free under
[GitHub's public-runner policy](https://docs.github.com/en/billing/concepts/product-billing/github-actions).
No Cloud Scheduler or always-on service is added. Nominally this is 96 invocations
and at most 480 sync attempts per day, excluding manual runs. Sustained full-duration
batches consume substantial CPU and Firestore operations; shared free quotas,
storage, network and artifact usage can still incur charges ([Cloud Run pricing](https://cloud.google.com/run/pricing)).
Monitor usage/budgets; these limits do not guarantee a zero bill.

Users see last success, last attempt, safe failure code, retry eligibility and a
30-minute stale/missing-sync warning. Eligibility is not promised execution time.
The display is a snapshot from connection status; refresh/reload to observe new
background results. Staff can inspect aggregate workflow summaries and structured
`calendar sync batch` logs without provider tokens, event details or user IDs.

CI tests cover synthetic cold HTTP startup, real JWT verification, ordinary-user
rejection, overlapping invocation, budgets, timeout-state recording, bounded
Firestore claims/deletion fences and existing PostgreSQL/Firestore sync fencing.
