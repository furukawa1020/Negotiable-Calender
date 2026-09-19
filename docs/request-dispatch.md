# Reliable request dispatch

Issue #135 implements real organization recipient selection and an atomic request creation command. It does not resolve Google OAuth verification or write events to Google Calendar.

## API contract

`POST /api/v1/requests` accepts `Idempotency-Key` (16–128 ASCII letters, digits, `_`, `-`). The authenticated actor and active organization scope the key. A SHA-256-derived request ID is stored, not the raw key. Never reuse a key for a different command.

- First committed command: `201` with the request and generated options.
- Same key and immutable input: `200`, `Idempotency-Replayed: true`, with the current request, including later cancellation/acceptance. Candidate generation is bypassed on a successful lookup.
- Same key with changed recipient, type, title, duration, deadline, sync preference or priority: `409`.
- Malformed key: `400`; lost membership: `403`; unavailable command storage or uncertain commit: `503`. Retry the exact command/key after an uncertain outcome.
- Timestamps are normalized to UTC microseconds for consistent Postgres/Firestore comparison.

Request, options, recipient in-app notification and creation audit commit together. Firestore uses a transaction with account deletion fences and membership reads. Postgres uses a transaction, command-scoped advisory lock and membership row locks. Collisions or failed writes roll back the entire transaction. Replays never reset notification read state or request lifecycle. Production stores use atomic creation even without a key, but keyless requests have no cross-call deduplication guarantee.

Authorization is checked again on replay. Keys do not grant access. Requests remain private to existing access rules; notification/audit messages do not copy request titles. Idempotency lasts while the request is retained; hard deletion does not retain a permanent command tombstone.

## Browser behavior and boundaries

The composer loads managers from the current organization's directory, excludes self, and preselects the person clicked in the organization list. It cannot submit while the directory is unavailable. Dates include a calendar date and local time; the API receives an absolute UTC timestamp. Requests use the API's `sync` enum, not `meeting`.

Double submission is blocked synchronously. An uncertain response retains the exact payload/key in the open composer for retry; edited input gets a new key. Changing account/workspace remounts the composer and ignores stale responses. There is intentionally no browser-persisted request content: closing/reloading discards the retry key. After an uncertain response, check Sent before closing or changing the command; this is not reload-safe draft recovery.

## Verification

- API tests cover replay, payload conflict, unavailable storage and denied membership.
- Firestore emulator and Postgres integration suites exercise concurrent duplicate submission, atomic side effects, read-state/lifecycle preservation and rollback on notification/audit collisions. Firestore additionally checks deletion fences.
- Composer tests cover recipient selection, directory failure/empty state, synchronous double submission, changed payload keys, lost-response replay and stale workspace responses.
- Playwright uses synthetic authenticated API responses to check organization-row selection and exact-key retry in the production build, without accessing real accounts or calendars.

No schema migration, additional paid service or external AI service is introduced.
