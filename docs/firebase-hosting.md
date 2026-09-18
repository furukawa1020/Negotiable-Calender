# Firebase public entry site

Issue #123, public-launch parent #121. This is a static introduction, not an app
origin migration and not Google OAuth verification completion.

## Isolation

- Existing project: `improve-production-management`.
- Dedicated site: `negotiable-calendar-480760`.
- Entry URL: `https://negotiable-calendar-480760.web.app` (check deployment before claiming live).
- Application: `https://negotiable-calendar-480760664246.asia-northeast1.run.app`.
- `firebase.json` explicitly names only the dedicated site and `hosting/public`.
- Do not deploy to the default site or either existing `yata-*` site.
- No app/API rewrites, Firebase SDK, analytics, forms, third-party assets or JavaScript.
- No billing, IAM, Firestore rules, app secrets or OAuth callback changes.
- `docs/public-launch-draft.md` is an unpublished draft and is outside the public directory.

Firebase strips cookies other than `__session` when proxying dynamic requests.
The app uses separate session/OAuth cookies, so a naive Cloud Run rewrite would
break authentication. A future app-origin migration needs explicit cookie/CSRF
design, Google callback registration, and real-account regression tests.

## Test and deploy

Run from the repository root using the existing authenticated Firebase CLI
(initially 15.25.1). Do not export refresh tokens or commit debug logs.

```powershell
$env:GOOGLE_CLOUD_QUOTA_PROJECT = 'improve-production-management'
python -B -m unittest discover -s .github/scripts -p 'test_*.py'
firebase hosting:sites:list --project improve-production-management --json
# One-time creation only; inspect sites first. Never delete/recreate another site.
firebase hosting:sites:create negotiable-calendar-480760 --project improve-production-management --non-interactive --json
firebase deploy --only hosting --config firebase.json --project improve-production-management --non-interactive --json
```

The five static-site tests run in the existing Production blueprint CI job.
The quota-project environment setting scopes API quota to the existing project;
it does not increase quotas or change billing plans. Without it, the CLI shared
consumer project returned Resource Manager 429 errors during initial setup.
After deploy, verify HTTPS index/CSS/404, response security headers, and the exact
Cloud Run link. Confirm `/api/v1/auth/session` and secret/draft paths on Hosting
return 404, not app data. Confirm the Cloud Run anonymous session remains 401.
Compare existing-site release IDs before/after deployment to detect accidental changes.

Roll back only this site's release via Firebase Hosting release history if needed.
Do not delete the site: site deletion is permanent and does not provide a safe rollback.

## Remaining public-launch gates

The user approved publishing `f.kotaro.0530@gmail.com` as the contact address.
The user approved the operator name `はたけ/Furukawa`. Retention details and final
policy/terms still require confirmation.
Google Branding, domain verification, audience and sensitive-scope review remain
separate work under #121; the free Firebase subdomain is not evidence of approval.
The page explicitly explains these limits. Do not remove that notice before real
ordinary-user consent and synchronization are verified.

Firebase Hosting offers a no-cost quota, not unlimited free use; this existing
project's billing plan is unchanged. Monitor aggregate project usage.

References:
- https://firebase.google.com/docs/hosting/multisites
- https://firebase.google.com/docs/hosting/manage-cache
- https://firebase.google.com/docs/hosting/usage-quotas-pricing
