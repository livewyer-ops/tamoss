#!/usr/bin/env sh
set -eu

script_dir="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=operator/test/chainsaw/scripts/lib.sh
. "$script_dir/lib.sh"

chainsaw_require_namespace

tenant="${1:-tamoss-s3}"
shift || true
if [ "$#" -eq 0 ]; then
  set -- tamoss
fi

tenant_exists() {
  kubectl -n "$NAMESPACE" get tenants.rustfs.com "$tenant" >/dev/null 2>&1
}

chainsaw_wait_for "RustFS Tenant/$tenant" 60 tenant_exists
chainsaw_patch_status \
  "$NAMESPACE" \
  tenants.rustfs.com \
  "$tenant" \
  "$(chainsaw_rustfs_ready_payload "$tenant")"

for tamoss_name in "$@"; do
  if [ "${CHAINSAW_RECONCILE_AFTER_STATUS_PATCH:-true}" = "true" ]; then
    chainsaw_reconcile_tamoss "$NAMESPACE" "$tamoss_name"
  fi
done
