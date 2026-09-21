# Primary-calendar least-privilege migration

Issue #145; public OAuth approval remains #121 and real-account acceptance #76.

## New request and compatibility

New Calendar consent requests exactly
`https://www.googleapis.com/auth/calendar.events.owned.readonly` with offline
access, explicit consent and PKCE. `include_granted_scopes=false` avoids asking
Google to combine prior grants. This is not a revocation operation or a guarantee
that Google will discard previously granted access.

The callback accepts this scope or the previously requested `calendar.readonly`
scope, together with a refresh token. It records the actual returned scopes, not
the requested minimum. Existing encrypted refresh tokens, sync cursors and stored
scope records are not rewritten, revoked or force-reconnected. Refresh requests
do not request a scope change. No database migration is required.

## Why this permission

| Feature | Google operation | Required data |
| --- | --- | --- |
| Busy-time import | GET calendars/primary/events | Event ID, times, status and transparency |
| Incremental sync | Same operation with syncToken | Changed and cancelled events, next cursor |
| Owner calendar view | Same operation with bounded dates | Event details shown only to the signed-in user |

Google lists owned-event read access among the scopes accepted by `events.list`.
The primary calendar belongs to its associated account. No calendar-list,
calendar properties, settings or ACL endpoints are used. Free/busy-only scopes
do not provide the owner's event details needed by the current product.
All-calendar event reading is unnecessary while the implementation is primary-only.
The grant can cover other owned calendars, but the implementation reads only primary.

Do not represent the narrower scope as a bypass of Google's verification or user
limits. The OAuth Data Access declaration and review must cover the actual new
scope before general-public acceptance. Do not delete a previously approved
scope from Console while legacy tokens and any in-flight old consent may exist.
Console declarations and approval status have not been verified by this change.

## Verification and rollback

- Unit tests assert the exact new request, PKCE/offline consent, and no combined
  authorization request. Callback tests accept new and legacy read grants and
  reject missing/offline-only/insufficient or lookalike grants.
- A synthetic provider test refreshes both grant types and runs full sync,
  incremental sync and private view exclusively against primary events.
- Firestore emulator integration runs the complete flow with the new grant,
  encrypted storage, incremental changes, publication and disconnect.
- Real Google consent, primary events including invitations/recurrence/all-day
  cases, continued legacy sync and non-allowlisted account access remain manual
  acceptance steps. Synthetic tests do not prove Google's live consent succeeds.
- Keep the dual-scope callback validator when reverting request behavior. A
  binary predating this migration rejects the new-only scope during callbacks;
  do not blindly roll back while users have in-flight new-scope consent.
- User-initiated disconnection revokes the stored token according to the existing
  implementation. Never bulk-revoke grants solely to force this migration.

## Official evidence

- [Scopes and their data access](https://developers.google.com/workspace/calendar/api/auth)
- [events.list accepted scopes](https://developers.google.com/workspace/calendar/api/v3/reference/events/list)
- [Primary calendar ownership](https://developers.google.com/workspace/calendar/api/concepts/events-calendars)
- [Incremental authorization](https://developers.google.com/identity/protocols/oauth2/web-server#incrementalAuth)
