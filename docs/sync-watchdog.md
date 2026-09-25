# Scheduled sync watchdog (#161, related #89)

`calendar-sync-watchdog.yml` observes the existing scheduler hourly at UTC minute
11, or by explicit dispatch. It runs only on main when CALENDAR_SYNC_ENABLED=true.
It uses contents:read/actions:read, no Google identity, cloud resources, calendar
calls, retry trigger, stored user data or write permission. Checkout credentials
are not persisted. Intentional shutdown skips both scheduler and watchdog.

The checker performs at most three GitHub REST GETs, each with a ten-second socket
timeout and a 1 MiB response limit, inside a two-minute job deadline. It reads the
known workflow metadata, at most 20 scheduled runs on main, and at most 10 jobs for
the latest recent successful scheduled candidate. The actual `sync` job and its
`Run one bounded synchronous batch` step must also have succeeded: a skipped job
inside a green workflow does not count. No response-provided URLs are followed;
HTTP redirects are rejected and bearer credentials never enter output.

An active workflow needs completed schedule success created within 90 minutes.
Manual dispatch, queued/in-progress/failed runs, another branch, or a recent rerun
of an old scheduled run cannot refresh that age. Creation time, not updated_at,
is intentionally conservative about queue/retry delay. Missing/stale success,
disabled workflow, invalid input, API/permission/rate-limit failures or unsuccessful
batch steps fail the watchdog. Output is fixed status codes and numeric ages only.
This reports inability to verify as failure rather than asserting an outage cause.

Operators should enable GitHub Actions failure notifications in their own account
and inspect a failed watchdog's fixed status plus the scheduler runs. Notification
delivery is NOT verified by a passing workflow. Repair missing configuration or
indexes using the existing runbooks; do not automatically rerun batches or broaden
permissions. A manual watchdog run is read-only, not a calendar synchronization.

## Limits and acceptance

This is a same-platform watchdog, NOT an independent external heartbeat. GitHub
outages, delayed/dropped schedules, or repository inactivity disabling both timers
can leave it silent. Neither a 90-minute threshold nor hourly cron promises a
90-minute detection SLA. Independent monitoring remains open under #89. No new
paid scheduler is provisioned; existing runner policy/quotas still apply.

Tests use synthetic clocks, API responses and failures. A successful live dispatch
validates API permission and current timer evidence, not automatic watchdog timer
execution or email delivery. Record these separately in the PR/issue. Empty sync
batches remain scheduler-only evidence, not real Google Calendar acceptance.

References: [workflow runs API](https://docs.github.com/en/rest/actions/workflow-runs),
[workflow API](https://docs.github.com/en/rest/actions/workflows),
[scheduled workflow limits](https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows#schedule).
