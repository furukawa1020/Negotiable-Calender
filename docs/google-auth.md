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
- Use a dedicated PostgreSQL database and run the API schema initialization before serving traffic.

## Endpoints

- `GET /api/v1/auth/google/login`
- `GET /api/v1/auth/google/callback`
- `GET /api/v1/auth/session`
- `POST /api/v1/auth/logout`
