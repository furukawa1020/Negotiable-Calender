# Google Calendar read-only sync

Calendar access uses separate consent from login. Negotiable Calendar asks for
`calendar.readonly` only and stores no title, description, attendee, location,
conference URL, or calendar name.

## Local setup

1. Enable Google Calendar API in the Google Cloud project used for login.
2. Add `http://localhost:8080/api/v1/calendar/google/callback` as an authorized
   redirect URI.
3. Set `GOOGLE_CALENDAR_REDIRECT_URL` to that URI.
4. Generate a dedicated 32-byte key with `openssl rand -base64 32` and set
   `CALENDAR_TOKEN_ENCRYPTION_KEY`.
5. Restart the API, sign in, and choose **Google Calendarを接続**.

The encryption key must be provided through a production secret manager and must
not be committed. Losing it requires users to reconnect. Rotation needs a
re-encryption migration before replacing the old key.

Manual sync imports a rolling window from 30 days ago through 90 days ahead.
After import, the API combines those private busy spans with the user's sharing
policy and active manual overrides, then replaces the public
15-minute projections for that window. Manual overrides are applied last.
Disconnecting stops publication of existing projections and deletes the encrypted grant and imported busy spans. Unknown time is not relabeled as available.


## Automatic incremental sync

The API starts a bounded background worker when Google Calendar credentials and
the token-encryption key are configured. New connections are scheduled
immediately; successful connections are checked every 15 minutes.

The first run imports the configured rolling window and saves Google's opaque
sync cursor. Later runs use that cursor, upsert changed recurring instances, and
delete cancelled instances. A `410 Gone` cursor expiry triggers one full-window
recovery. Public projections are rebuilt before the new cursor is committed, so
a failed rebuild does not advance the cursor. Multi-batch Firestore publication atomicity remains tracked in #88.

Workers claim due connections with PostgreSQL `FOR UPDATE SKIP LOCKED` and a
two-minute scheduling reservation. Manual and background executions additionally
acquire a per-connection execution lease, described below. Each Google operation is bounded by the worker timeout. Temporary
failures use exponential backoff with deterministic jitter, capped at six
hours. A revoked grant is excluded from future claims and the UI requests an
explicit reconnect.

The connection API exposes only sanitized health fields:

- `lastSyncedAt`
- `lastAttemptAt`
- `nextAttemptAt`
- `lastErrorCode`
- `reconnectRequired`

Refresh tokens remain AES-GCM encrypted. Access tokens, refresh tokens, sync
cursors, raw Google responses, event titles, attendees, locations, and
descriptions are never returned or logged.


## Manager private calendar view

Authenticated managers can load day, week, or month ranges from
`GET /api/v1/me/private-events?from=...&to=...`. The route has no user ID
parameter: the server derives the owner exclusively from the verified session.
Ranges must be RFC 3339, ordered, and no longer than 45 days.

For this self-only response, the API refreshes a short-lived Google access token
and streams the requested event details to the owner with `Cache-Control:
no-store`. Details are not written to PostgreSQL. Organization, projection,
coordination, audit, and notification APIs continue to use only privacy-safe
projection data and cannot import this DTO.

The production Web client restores an existing server session on startup.
Unauthenticated production visitors see only the Google sign-in gate. Fixed
sample events are rendered only by the explicit development demo mode.

## Disconnect publication and integration verification

PostgreSQL removes public projections, private events, and the connection in one
transaction. Firestore first writes a durable per-user `projectionControls/calendarDisconnected`
marker: public projection reads return an empty set while it exists, even when
subsequent cleanup fails. Read errors fail closed. Reconnecting alone does not
remove it; successful sync completion removes it in the same batch as the
connection completion update. Account deletion cleans the marker. The web UI
clears displayed events, selected details, and projections after disconnect.

The Firestore emulator integration test uses the real GoogleProvider parser with
a synthetic HTTP transport: consent redirect, PKCE exchange, single-use callback,
encrypted grant, full/incremental sync, cancellation, actual projection rebuild,
disconnect, failed reconnect sync, and successful publication resumption.
PostgreSQL tests inject a delete failure to verify transaction rollback and user
isolation. These tests require no real OAuth client and do not verify live consent.

Remaining production gaps: OAuth provisioning/live verification (#76),
multi-batch atomicity (#88), and reliable scheduled
sync when Cloud Run scales to zero (#89). The deployed demo and synthetic tests
must not be described as a completed real Google Calendar integration.

## Sync ownership and lifecycle races

Every manual or background sync acquires a random execution ID with a two-minute
lease before reading Google. Its context carries this ID to event writes, public
projection writes, and success/failure status updates. Each database transaction
checks the ID and its expiry while holding the connection's write lock. Firestore
checks every chunk; PostgreSQL locks the per-user lifecycle and connection row.
The ID and lease timestamps are not included in connection JSON responses.

A second execution returns HTTP 409 while an owner is active. Reconnecting resets
ownership. Disconnecting invalidates ownership before cleanup, so an old Google
response cannot repopulate events or change the new grant's sync/failure state.
A crashed execution may be retried after the lease expires; the old execution
remains fenced even if it resumes later. Context cancellation may leave a lease
until expiry rather than write failure state with an already-cancelled context.

Firestore disconnect uses its own two-minute cleanup lease. Reconnect is rejected
while cleanup is in progress. After a cleanup failure, retry disconnect after the
lease expires, then reconnect; old cleanup batches are fenced if a retry takes
over. The public block remains until a valid new sync completes. PostgreSQL uses
one transaction for disconnect, with the same per-user lock as sync and reconnect.

### Schema and rollout

PostgreSQL `EnsureBackgroundSchema` adds `sync_lease_id` (empty by default) and
`sync_lease_until` (nullable) without deleting existing data. Firestore missing
lease fields are interpreted as no active owner. No new composite index is used.

Do not run old unfenced and new fenced workers against a live connected account
at the same time: drain/stop old workers before enabling real-account traffic.
Rollback to an unfenced revision requires stopping new sync activity as well;
retaining the additive SQL columns is safe. The current published environment
remains demo mode with real OAuth unconfigured, so deploy fencing before #76.

This prevents obsolete writers, not whole-window atomic replacement. Partial
successful chunks before an execution fails are still covered by #88.
