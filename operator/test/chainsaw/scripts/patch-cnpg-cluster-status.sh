#!/usr/bin/env sh
set -eu

script_dir="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=operator/test/chainsaw/scripts/lib.sh
. "$script_dir/lib.sh"

chainsaw_require_namespace

cluster="${1:-tamoss-db}"
tamoss_name="${2:-tamoss}"
payload="${CNPG_STATUS_JSON:-$(chainsaw_cnpg_ready_payload)}"

chainsaw_patch_status \
  "$NAMESPACE" \
  clusters.postgresql.cnpg.io \
  "$cluster" \
  "$payload"

if [ "${CHAINSAW_RECONCILE_AFTER_STATUS_PATCH:-true}" = "true" ]; then
  chainsaw_reconcile_tamoss "$NAMESPACE" "$tamoss_name"
fi
