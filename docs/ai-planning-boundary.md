# Opt-in AI proposal boundary (#127, parent #126)

## Status and infrastructure decision

This is an inactive Go library, not a deployed AI feature. The user requested
Google Cloud and Firebase as the base and no paid AI operation. Cloudflare was
considered and then withdrawn before committing an adapter. There is no network
adapter, API route, AI SDK, new credential, or production configuration in this PR.
All inference tests use synthetic in-memory providers. Existing deterministic
scheduling remains unchanged and must not be advertised as AI.

Google's Gemini unpaid-service data terms and paid-service billing differ. Do not
send real Calendar data to a training-eligible unpaid service or enable a billable
API to satisfy the request. A suitable zero-cost Google configuration has NOT been
verified. Account eligibility, data-use terms and a hard cost boundary are activation
gates, not things an application budget alert guarantees.

## Contract

1. Authenticate the account and authorize its workspace before calling the library.
2. Supply a trusted Loader that checks account deletion, Calendar disconnection,
   source freshness, policy versions and current booking constraints. Never accept
   Snapshot from browser JSON. Its revision must cover every relevant input.
3. Preview serializes only enumerated preference and up to 12 candidate time ranges.
   Provider IDs are temporary c1..c12. User/workspace/request/calendar IDs, event
   titles/descriptions/attendees, tokens and free text are not in the provider input.
   The signed digest includes internal identity and state without disclosing them.
4. Show provider, model, data-policy version, exact payload and retention/training
   terms before consent. The user must affirmatively choose to send this preview.
   Google Calendar permission is not consent to AI processing.
5. Generate requires a two-minute-or-shorter signed consent, true consent flag and
   an exact fresh snapshot match. Provider/model/terms/preference changes invalidate
   prior consent. Authentication/CSRF remain the future HTTP adapter's responsibility;
   the signed token is not an authentication credential.
6. A required Permit atomically consumes the preview once, confirms zero-cost
   eligibility, and reserves user/account budget. No permissive implementation is
   supplied. Persist this in Firestore and bind its records to account deletion.
   A consumed attempt is not automatically refunded after failure or timeout.
7. Reload after the permit and after inference. Changed/deleted/disconnected/stale
   state discards the result. In-flight changes cannot undo data already sent; the
   implementation must not promise instantaneous revocation of an external request.
8. Accept only 1–3 distinct IDs from the trusted candidate list. Never return raw
   provider text/errors or execute model instructions. The result is only a proposal.
   Human acceptance still uses existing transactional conflict/freshness checks.

The current preference choices (earlier/later) are a deliberately narrow transport
contract, not a claim of useful model quality or a completed natural-language planner.
An adapter may wrap the previewed data in a fixed, reviewed system instruction; it
must not attach undisclosed calendar details, tracking identifiers or tools.

## Integration work still required

- Approved Google inference configuration and model/license/data-term evaluation.
- Real authorized storage loader, deletion-aware durable permit and cost controls.
- HTTP endpoints with identity/CSRF/rate limits and contextual consent UI.
- Bounded HTTPS provider adapter: timeout, response cap, no redirects/retries, strict
  output validation and no sensitive request/response logging.
- Live synthetic evaluation before real-account opt-in testing; failure and limit
  handling must label the deterministic fallback as non-AI.
- Accurate published policy and Google OAuth review, tracked separately in #121/#125.

Tests cover minimized payload, identity isolation, changed provider/terms/model,
expiry, stale input before/during generation, one-use gate, denied budget, malformed
IDs, malicious provider mutation, cancellation and sanitized errors. They do not
prove the eventual database permit's atomicity or a provider's actual free plan.

Official references checked 2026-09-19:
- https://ai.google.dev/gemini-api/terms
- https://ai.google.dev/gemini-api/docs/pricing
- https://developers.google.com/workspace/workspace-api-user-data-developer-policy
