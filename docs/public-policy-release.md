# Public privacy policy and terms (#150)

The operator requested preparation and Firebase publication on 2026-09-23.
This authorizes these pages, not Google verification submission, OAuth settings,
new cloud AI use, a domain purchase, or a claim of legal review/compliance.
The user-owned `docs/public-launch-draft.md` remains untouched and unpublished.

## Canonical links

- Home: https://negotiable-calendar-480760.web.app/
- Privacy: https://negotiable-calendar-480760.web.app/privacy.html
- Terms: https://negotiable-calendar-480760.web.app/terms.html

Firebase serves static pages only. No app-origin/cookie migration or rewrites.
The app links from signed-out login, Calendar consent/reconnect and the connected
account menu. Links open a labeled separate tab with no opener/referrer.

## Evidence reviewed

- Identity: `apps/api/internal/auth/model.go` and Google auth handlers.
- Calendar: `calendar/provider.go`, `model.go`, `private_events.go`,
  `firestorestore/calendar.go`; primary-only view, busy persistence and cleanup.
- AI: `web/src/localPlanning.ts`, `LocalPlanningPanel.tsx` and
  `docs/on-device-planning.md`. Only opt-in on-device candidate comparison is
  covered. No cloud inference, generic training or blanket future AI consent.
- Deletion: `docs/account-deletion.md`, including retained lifecycle markers,
  operator retry and historical orphan limits. No instantaneous erasure promise.
- 2026-09-23 read-only GCP checks: global Logging `_Default` 30 days,
  `_Required` 400 days; default Firestore database in asia-northeast1 has PITR
  disabled, no backup schedules, no listed backups in asia-northeast1. This
  does not establish absence of manual exports or provider replicas.
- User screenshots confirmed the correct project's Audience is Testing, with
  publishing disabled pending Branding; homepage/privacy/terms fields empty.

## Verification and deployment

Run Python deployment/static tests, Web lint/unit/build, and Playwright tests.
`public-policy.spec.ts` checks both page widths, navigation, login and each
Calendar connection state. No real-user Calendar data is used in tests.
Deploy only `firebase.json`'s explicit `negotiable-calendar-480760` site after
the PR checks pass. Check both policy bodies and headers live, compare with
local bytes, confirm private/draft/secret paths stay 404 and other sites retain
their release IDs. Cloud Run app links deploy through the existing gated CI.

## Remaining gates

#125 tracks final policy operations/review; #121 tracks domain ownership,
Branding/Audience/Data Access and Google decision; #76 tracks real non-allowlisted
consent and sync. Static publication closes none of these by itself. The newly
published terms preserve statutory rights without inventing a governing court,
blanket liability waiver or an automatic paid plan. Seek qualified review before
claiming legal compliance. Changes to actual retention, subprocessors or AI data
flows require policy review and, where needed, fresh consent before processing.

Official requirements reviewed:
- https://developers.google.com/terms/api-services-user-data-policy
- https://support.google.com/cloud/answer/15549049?hl=en
