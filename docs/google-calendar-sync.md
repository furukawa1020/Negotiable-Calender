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
policy and active manual overrides, then atomically replaces the public
15-minute projections for that window. Manual overrides are applied last.
Disconnecting deletes both the encrypted grant and imported busy spans.


## Automatic incremental sync

The API starts a bounded background worker when Google Calendar credentials and
the token-encryption key are configured. New connections are scheduled
immediately; successful connections are checked every 15 minutes.

The first run imports the configured rolling window and saves Google's opaque
sync cursor. Later runs use that cursor, upsert changed recurring instances, and
delete cancelled instances. A `410 Gone` cursor expiry triggers one full-window
recovery. Public projections are rebuilt before the new cursor is committed, so
a failed rebuild is retried idempotently instead of publishing partial state.

Workers claim due connections with PostgreSQL `FOR UPDATE SKIP LOCKED` and a
two-minute lease, preventing concurrent API replicas from processing the same
connection. Each Google operation is bounded by the worker timeout. Temporary
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
