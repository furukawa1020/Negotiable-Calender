# Booking response contracts

Issue #179, part of #102.

Inbox acceptance now requires an acknowledgement matching the request ID,
accepted status, and chosen option ID. HTTP 2xx alone is not success. JSON is
decoded before local state changes and the account/workspace lifetime is checked
again after decoding. Invalid/empty bodies retain the original row and ask the
user to check the latest state; no automatic retry is performed.

Rescheduling uses a runtime decoder rather than a TypeScript-only cast. It checks
the request/workspace and participant binding, required rendered fields, positive
integer duration, parseable dates, option types/IDs/uniqueness, valid meeting
ranges, a selected meeting for accepted requests, and proposal references.
Optional rendered text fields must also be strings. Invalid payloads do not
replace either list, clear retry proposal IDs, or supersede pending list reads.
Existing transport deadlines and account/workspace fences remain in effect.

The API reads the request after applying a reschedule transaction, so another
operation may already be visible. A structurally valid later cancellation (or
other current state) is applied with a neutral latest-state notice. A submitted
command is announced as current only if its proposal ID, expected option, status,
and selected option match the snapshot. This is not server transaction versioning.

Tests cover malformed/wrong-context payloads, successful acknowledgement/replay,
retry recovery, late acceptance JSON after account/workspace changes, all four
reschedule outcome types, and a concurrent cancellation in both request lists.
The decoder checks response safety; it does not replace backend authorization or
availability checks. The inbox acceptance transport still has no new deadline
in this change. Real Google OAuth acceptance is not exercised.
