# On-device candidate comparison

Issue #129 is one bounded delivery within #126, not completion of calendar-aware AI or Google public verification (#121).

## Deployment and cost boundary

- Hosting remains Firebase Hosting (intro) and Google Cloud Run (application/API).
- The browser uses Google's Gemini Nano through the native Chrome `LanguageModel` Prompt API. No Firebase AI Logic SDK, Gemini cloud endpoint, Cloudflare provider, API key, billing configuration, origin trial, or experimental flag is installed/enabled by this change.
- The direct native API avoids setting up a cloud inference fallback. Unsupported devices keep the ordinary candidate/approval UI. Feature availability is checked at runtime; no universal browser or mobile support is promised.
- On-device inference has no API usage fee. Initial model downloads consume network/storage; existing Cloud Run/Firestore traffic retains its existing costs/free-tier limits. This does not guarantee the entire deployed service is always free.
- Model creation only follows the user's explicit checkbox and click. Download/model preparation can exceed the 90-second inference attempt timeout; the user can close/retry. Models are managed by Chrome, not stored by the application.

Official references (reviewed 2026-09-19):
- https://developer.chrome.com/docs/ai/prompt-api
- https://firebase.google.com/docs/ai-logic/hybrid/web/get-started
- https://firebase.google.com/docs/ai-logic/pricing

## Data and authority

The authenticated inbox target can request `GET /api/v1/requests/{requestId}/planning-preview`. It requires the active workspace to match and a current membership. Real-session middleware supplies the identity; client identity headers are not trusted in production.

Only suggested, unexpired meeting options with complete current target projection coverage and no accepted in-app booking conflict for either participant are eligible. Requester external calendars are not fetched by this endpoint. Existing projection freshness gates apply. Missing/failed/conflicting source data fails closed.

The response contains a SHA-256 source revision, an expiry (at most two minutes, shortened by projection expiry/request deadline/start time), and 1–12 opaque candidate IDs with start/end timestamps. The request/projection/booking records themselves never leave the endpoint. No reservation is created.

The preview response/prompt is bounded (12 candidates and a 31-day candidate span). The endpoint also rejects source result slices above 10,000 projection rows or 5,000 requests per participant. These checks occur **after store reads**, so they are not database query/read-cost limits. Three preview reads occur per successful comparison. Further indexed/limited query work remains relevant at scale.

Before sending anything to the model, the user sees the candidate times, an enumerated earlier/later preference, what is excluded, local processing/download details, and a consent checkbox. The model receives only opaque IDs, timestamps, and the enumerated preference, not source revision, names, titles, descriptions, locations, real event IDs, attendees, or free text. No prompt/result is persisted, logged, or submitted to a server AI endpoint by this feature. No model training integration is added.

The browser reloads the source before and after inference and rejects changed revisions/candidates. It validates expiry and accepts only 1–3 unique known IDs with no extra output fields. Closing, unmounting (including identity/workspace changes), timeout, and expiry abort pending work, destroy an available model session, and clear display. A late-created session is destroyed without prompting. Raw model/provider errors are never shown.

These reads are not an atomic snapshot or lock. Changes after a preview remain possible; the existing explicit transactional meeting acceptance is the sole booking path. AI ranking never creates/changes calendar events or approves a meeting.

## Verification and remaining acceptance

Automated Go tests exercise the routed endpoint's owner/workspace/membership boundary, source errors, stale/gapped/busy projections, accepted/corrupt booking conflicts, data minimization, expiry clamping and revision changes. Frontend tests mock the native API and exercise explicit consent, unsupported environments, source changes before/after inference, invalid output, no fetch-based model calls, cancellation, late model creation, timeout, and result expiry.

**Mocked tests are not a real Gemini Nano run.** Before declaring this AI feature generally ready, verify on a supported Chrome desktop/device with a real signed-in account and a legitimate suggested request:

1. Open the inbox candidate comparison. Confirm the actual candidate list and exclusions before consenting. Opening alone must not create a model session.
2. Consent and click; verify any Chrome model-download permission/progress, successful local output, and only ordinary same-origin preview requests from application code (Chrome itself may download its model).
3. Change source availability during inference and confirm rejection; close/navigate/switch workspace while pending and confirm no stale result reappears.
4. Repeat on an unsupported device: ordinary approval remains available and no cloud inference occurs.
5. Confirm a selected meeting only through the existing approval button; verify transactional conflict handling separately.

Actual model quality, supported-device behavior and real-account end-to-end Google Calendar sync remain unverified here. The function compares existing valid options; it does not read full private event details or create new options from natural-language instructions. Public privacy-policy work (#125) and Google OAuth verification (#121) remain open.
