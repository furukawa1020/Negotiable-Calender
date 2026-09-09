# Google login setup

Negotiable Calendar uses the OAuth 2.0 authorization-code flow with PKCE S256 and a one-time server-side state record.

## Google Cloud configuration

1. Create an OAuth 2.0 Web application client in Google Cloud Console.
2. Add `http://localhost:8080/api/v1/auth/google/callback` as an authorized redirect URI for local development.
3. Set `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`, and `GOOGLE_REDIRECT_URL` in `.env`.
4. Restart the API service.

The login flow requests only `openid profile email`. Google Calendar permission is intentionally requested later, when the user explicitly connects a calendar.

## Production requirements

- Set `DEMO_MODE=false`; production middleware removes all incoming demo identity headers.
- Keep `COOKIE_SECURE=true` and serve both Web and API over HTTPS.
- Register the exact production callback URI with Google.
- Store the client secret in a managed secret store, never in Git.
- Configure the selected storage backend: PostgreSQL locally, or Firestore for the current Cloud Run deployment.

## Endpoints

- `GET /api/v1/auth/google/login`
- `GET /api/v1/auth/google/callback`
- `GET /api/v1/auth/session`
- `POST /api/v1/auth/logout`

## Cloud Run real-account setup

Register both exact redirect URIs on the same Google OAuth Web application client:

- `https://negotiable-calendar-480760664246.asia-northeast1.run.app/api/v1/auth/google/callback`
- `https://negotiable-calendar-480760664246.asia-northeast1.run.app/api/v1/calendar/google/callback`

Set `GOOGLE_REDIRECT_URL` and `GOOGLE_CALENDAR_REDIRECT_URL` to these URLs,
and `WEB_ORIGIN` to `https://negotiable-calendar-480760664246.asia-northeast1.run.app`.
Supply `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`, and the base64-encoded
32-byte `CALENDAR_TOKEN_ENCRYPTION_KEY` through the runtime configuration
and Secret Manager. Preserve the encryption key across releases.

Before setting `DEMO_MODE=false`, configure all of the above. The API validates
real-account settings before storage initialization or serving requests.
Missing credentials, invalid callback paths, query strings, URL credentials,
public HTTP, insecure HTTPS cookies, and invalid encryption keys prevent startup.
Validation errors identify setting names without printing their values.

HTTP loopback URLs are allowed for local development. Only explicit
`DEMO_MODE=true` bypasses these real-account requirements. A missing
`DEMO_MODE` uses real-account validation; misspelled values are rejected.

Startup validation checks configuration syntax and completeness, not whether
Google accepts the client or the consent screen is published. Verify login,
Calendar consent, sync, and logout with real test accounts before declaring
real-account production ready. The currently published service remains a demo.
