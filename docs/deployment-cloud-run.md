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
`/ready`, and the demo-mode session endpoint. Cloud Run also probes `/health`
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
