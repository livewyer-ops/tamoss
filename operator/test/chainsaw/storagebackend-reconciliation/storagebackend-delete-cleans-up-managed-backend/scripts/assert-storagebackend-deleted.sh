#!/usr/bin/env sh
set -eu

: "${NAMESPACE:?NAMESPACE is required}"

backend_id="11111111-1111-5111-8111-111111111111"

end=$(($(date +%s) + 180))
while kubectl -n "$NAMESPACE" get storagebackend archive >/dev/null 2>&1; do
  if [ "$(date +%s)" -ge "$end" ]; then
    echo "timed out waiting for StorageBackend deletion" >&2
    exit 1
  fi
  sleep 2
done

row_count="$(
  kubectl -n "$NAMESPACE" exec deployment/postgresql -- sh -ec \
    "PGPASSWORD=tams psql -U tams -d tams -tAc \"SELECT COUNT(*) FROM tamoss_storage_backends WHERE id = '$backend_id'::uuid\""
)"

if [ "$row_count" != "0" ]; then
  echo "expected archive storage backend row to be deleted, got $row_count" >&2
  exit 1
fi

name="awscli-$(date +%s%N)"
if kubectl -n "$NAMESPACE" run "$name" \
  --quiet \
  --rm \
  -i \
  --restart=Never \
  --image=amazon/aws-cli:2.32.34 \
  --env=AWS_ACCESS_KEY_ID=rustfsadmin \
  --env=AWS_SECRET_ACCESS_KEY=rustfsadmin \
  --env=AWS_DEFAULT_REGION=us-east-1 \
  --command -- aws --endpoint-url http://rustfs-svc:9000 s3api head-bucket --bucket archive >/tmp/archive-head.out 2>/tmp/archive-head.err; then
  echo "expected archive bucket to be deleted" >&2
  exit 1
fi
