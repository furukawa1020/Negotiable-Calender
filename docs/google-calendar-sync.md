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
Disconnecting deletes both the encrypted grant and imported busy spans.
