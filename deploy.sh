#!/bin/bash
# Deploys sitemap-svc (cmd/sitemap-svc) to Cloud Run (us-central1). Scale-to-zero,
# IAM-authenticated, no project roles on the runtime identity — it fetches
# sitemaps from the public internet and needs nothing from GCP.
#
# Mirrors the webrisk-svc deploy conventions (same project, region, Artifact
# Registry repo, per-service no-role SA, --no-allow-unauthenticated).
#
# Sizing differs from robots-svc, because the work does. A walk can pull dozens
# of documents of up to 50 MiB each and parse them into a typed AST, so:
#   * memory is 2Gi, and CONCURRENCY is low — concurrent walks multiply peak
#     memory, and 80-per-instance would OOM on large sitemaps;
#   * TIMEOUT is 900s, not the 300s default. A real walk of a large site
#     measured ~291s against these services, which the default would kill.
#
# Knobs (env vars):
#   MAX_INSTANCES=4   upper bound on concurrent instances
#   CONCURRENCY=4     walks per instance (each one can be memory-heavy)
#   TIMEOUT=900       request deadline, seconds
#   FETCH_CONC=8      concurrent sitemap fetches within one walk
set -euo pipefail

PROJECT="speax-498608"
REGION="us-central1"
IMAGE="us-west1-docker.pkg.dev/${PROJECT}/embedder/sitemap-svc"
SA="sitemap-svc@${PROJECT}.iam.gserviceaccount.com"
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

MAX_INSTANCES="${MAX_INSTANCES:-4}"
CONCURRENCY="${CONCURRENCY:-4}"
TIMEOUT="${TIMEOUT:-900}"
FETCH_CONC="${FETCH_CONC:-8}"

gcloud services enable run.googleapis.com --project="${PROJECT}"

# Runtime identity with no project roles: the service only makes outbound HTTP.
gcloud iam service-accounts describe "${SA}" --project="${PROJECT}" >/dev/null 2>&1 ||
  gcloud iam service-accounts create sitemap-svc --project="${PROJECT}" \
    --display-name="sitemap-svc runtime (no project roles)"

# Artifact Registry push needs docker configured for this host (idempotent;
# rewrites ~/.docker/config.json only when the helper is missing).
gcloud auth configure-docker us-west1-docker.pkg.dev --quiet

SHA=$(git -C "${HERE}" rev-parse --short HEAD)
DOCKER_BUILDKIT=1 docker build --platform linux/amd64 \
  -t "${IMAGE}:latest" -t "${IMAGE}:${SHA}" "${HERE}"
docker push "${IMAGE}:latest"
docker push "${IMAGE}:${SHA}"

gcloud run deploy sitemap-svc --project="${PROJECT}" --region="${REGION}" \
  --image="${IMAGE}:${SHA}" \
  --service-account="${SA}" \
  --no-allow-unauthenticated \
  --min-instances=0 --max-instances="${MAX_INSTANCES}" \
  --memory=2Gi --cpu=2 --concurrency="${CONCURRENCY}" \
  --timeout="${TIMEOUT}" \
  --args="-request-timeout=$((TIMEOUT - 60))s,-concurrency=${FETCH_CONC}"

URL=$(gcloud run services describe sitemap-svc --project="${PROJECT}" \
  --region="${REGION}" --format='value(status.url)')
echo
echo "sitemap-svc: ${URL}"
echo "Grant callers: gcloud run services add-iam-policy-binding sitemap-svc \\"
echo "  --region=${REGION} --member=serviceAccount:<caller-sa> --role=roles/run.invoker"
