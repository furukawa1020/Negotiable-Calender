# Cloud Run production deployment

Production runs as one same-origin container on Cloud Run with Firestore Native.

- URL: <https://negotiable-calendar-480760664246.asia-northeast1.run.app>
- Region: `asia-northeast1`
- Cloud Run service: `negotiable-calendar`
- Firestore database: `(default)`
- Runtime service account: `negotiable-calendar-run@improve-production-management.iam.gserviceaccount.com`

The service uses request-based billing, zero minimum instances, and one maximum
instance. These settings keep idle usage at zero and cap accidental scale-out.
Cloud Run and Firestore remain free only while usage stays inside their free
quotas; quota settings are not an absolute spending cap.

## Automatic deployment

`.github/workflows/deploy-cloud-run.yml` verifies the API and Web application,
builds one immutable image with provenance and an SBOM, pushes it to Artifact
Registry, deploys that exact digest, and smoke-tests the public service.

The workflow authenticates with GitHub OIDC and Google Workload Identity
Federation. It has no downloadable service-account key and needs no GitHub
secret. The provider accepts only this repository's `main` branch. The deployer
identity can write images and update Cloud Run; the application runs under a
separate identity that can access Firestore.

Every merge to `main` triggers a deployment. A maintainer may also use **Run
workflow** on the GitHub Actions page. The `production` concurrency group avoids
overlapping deployments.

## Health and rollback

After deployment the workflow requires successful responses from `/health`,
`/ready`, and the session endpoint with the configured authentication mode. Cloud Run also probes `/health`
and `/ready` on the running revision.

To roll back, deploy a known-good digest from Artifact Registry:

```bash
gcloud run deploy negotiable-calendar \
  --project=improve-production-management \
  --region=asia-northeast1 \
  --image=asia-northeast1-docker.pkg.dev/improve-production-management/cloud-run-source-deploy/negotiable-calendar@sha256:DIGEST
```

Do not place OAuth secrets in the repository or workflow. The public deployment
currently uses demo mode. Enabling real Google OAuth requires creating an OAuth
client and storing its values in Secret Manager before setting `DEMO_MODE=false`.

## Authentication configuration survives releases

Routine deployments update only infrastructure environment variables. They preserve
the existing `DEMO_MODE`, OAuth settings, and Secret Manager references. Before
building an image, the workflow requires an existing service with exactly one explicit
`DEMO_MODE=true` or `DEMO_MODE=false` value. It fails before deployment if the
service is missing or the value is invalid. Initial service provisioning is separate.

The post-deploy check parses session JSON and verifies that the configured mode
was retained and that an anonymous request remains unauthenticated. Each HTTP
request has connection and total timeouts. OIDC permission is limited to the
deployment job, which runs only for `main`.

For real-account use, complete Google OAuth client setup and Secret Manager
configuration before changing `DEMO_MODE` to `false`. A passing demo deployment
does not verify login or Calendar consent with a real Google account.

## Bounded history reads

Notification lists query the latest 100 documents and organization audit lists
query the latest 200, ordered by `CreatedAt DESC, __name__ DESC` in Firestore.
Document IDs equal the stored event IDs, preserving deterministic same-time ordering.
This uses the default descending single-field index on `CreatedAt`; keep that
index enabled for the `notifications` and `auditLogs` collections. See the
[Firestore index ordering documentation](https://firebase.google.com/docs/firestore/query-data/index-overview#default_ordering_and_the_name_field).

These limits bound returned document reads, not total project costs. Older
records remain stored; this is not a retention or deletion policy. The emulator
release gate covers over-limit history, time/ID ordering, principal isolation,
empty arrays, and an undecodable old record that must remain outside the query.
