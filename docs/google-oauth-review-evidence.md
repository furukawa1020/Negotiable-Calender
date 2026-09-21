# Google Calendar OAuth: review evidence and 403 triage

Tracked by #121 (Google approval), #125 (public policy), #76 (real-account
acceptance), and #143 (consent recovery/disclosure). This is a preparation
document, not evidence of submission, approval, or successful real-user consent.

## Confirmed application configuration

- Project: improve-production-management (480760664246).
- Public login redirect matches the supplied Web OAuth client and expected
  callback. Calendar and login use the same GOOGLE_CLIENT_ID.
- Calendar API is enabled. Cloud Run is deployed, but deployment is not an
  OAuth publishing or verification status.
- Login scopes: openid profile email.
- Calendar scope: https://www.googleapis.com/auth/calendar.readonly.
- Calendar callback:
  https://negotiable-calendar-480760664246.asia-northeast1.run.app/api/v1/calendar/google/callback
- Google consent uses offline access, explicit consent, PKCE S256, and a
  one-time flow bound to the authenticated app user.
- Audience, publishing status, user-cap usage, and verification status have
  **not** been directly inspected. The user reports In production. Earlier
  notes asserting that the project is still Testing are not verified facts.

## Scope justification draft

Negotiable Calendar displays the signed-in user's primary-calendar events to
that user and derives busy-time intervals for scheduling requests. It reads
event times, recurrence changes, cancellations and availability, and displays
event details only in the owner's private calendar view. Event details are not
included in organization-facing availability projections.

The free/busy-only permission is insufficient for the implemented owner event
view and event-level incremental synchronization. No calendar write operation
is implemented. The current scope also permits broader read access than the
primary-calendar-only implementation uses. Before submission, document the
minimum-scope review; consider calendar.events.readonly with equivalent coverage
and regression tests, rather than claiming calendar.readonly is the narrowest
possible scope. Do not change scopes solely to evade verification.

This text is a reviewer draft describing implementation, not an attestation
of compliance or an approved privacy policy.

## Demonstration recording checklist (not yet recorded)

Use a real consenting account with a calendar prepared for demonstration. Avoid
showing unrelated private appointments, credentials, tokens or complete callback
URLs. Synthetic tests do not substitute for the live demonstration.

1. Open the logged-out site and approved homepage/privacy links.
2. Sign in using Google and show the real user's profile.
3. Open the account menu and explain the contextual Calendar access disclosure.
4. Start Calendar connection and show Google's consent screen and exact scope.
5. Complete consent, show connection state, and sync the primary calendar.
6. Show the owner's event detail and the organization's separate detail-free
   availability view. Verify with a second, appropriately authorized account.
7. Change a demonstration event in Google Calendar and show updated sync state.
8. Disconnect; show removal of imported busy data and stopped publication.
9. Show the account deletion flow and explain retained deletion tombstones and
   other retention limits accurately; do not claim instantaneous total erasure.

Blockers: confirmed OAuth Console status; approved public policy and domain
ownership evidence; live consent/sync acceptance; actual video; Google decision.
Do not submit an empty video link or claim any unchecked step passed.

## Distinguish failure locations

| Location / observation | Interpretation and next action |
| --- | --- |
| Google-hosted access_denied screen says testing | Inspect Audience for the exact client project; do not assume the deployed backend establishes OAuth publication. |
| Audience says In production but Google denies access | Record fresh error text, verification/scope decisions and user-cap status. Do not assume adding a tester will fix public access. |
| Google returns error=access_denied to the app | Validate and consume state, clear flow cookie, show fixed denial notice, preserve any existing connection. This can also be user cancellation; do not label it definitely a testing restriction. |
| Token exchange fails | Show fixed exchange-failed notice; check sanitized server diagnostics. Never reflect provider text, code or tokens. |
| Exchanged grant lacks read scope or refresh token | Show permission-required notice; preserve any existing connection; explicit re-consent is available. |
| Callback has missing/expired/wrong-user state, or is replayed | Reject without exchanging a token or altering connection. Start a fresh flow. |

Google may block consent without redirecting back. In that case the app cannot
capture that denial automatically; the account menu and logged-out page provide
the help entry. No client-side error message proves the app is verified.

Ask users for the timestamp, error text and whether they were on Google or the
app. Never ask for full URL, code, cookies, tokens or client secrets. Do not log
OAuth query strings in additional diagnostics.

## Official references

- [Audience / publishing / user cap](https://support.google.com/cloud/answer/15549945?hl=en)
- [Brand verification](https://developers.google.com/identity/protocols/oauth2/production-readiness/brand-verification)
- [Sensitive-scope verification](https://developers.google.com/identity/protocols/oauth2/production-readiness/sensitive-scope-verification)
- [Calendar scopes](https://developers.google.com/workspace/calendar/api/auth)
