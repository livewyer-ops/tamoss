#!/usr/bin/env sh
set -eu

chainsaw_require_namespace() {
  : "${NAMESPACE:?NAMESPACE is required}"
}

chainsaw_wait_for() {
  description="$1"
  timeout="$2"
  shift 2
  end=$(($(date +%s) + timeout))
  until "$@"; do
    if [ "$(date +%s)" -ge "$end" ]; then
      echo "timed out waiting for $description" >&2
      return 1
    fi
    sleep 2
  done
}

chainsaw_reconcile_tamoss() {
  namespace="$1"
  name="$2"

  kubectl -n "$namespace" annotate tamoss "$name" \
    "chainsaw.livewyer.io/tick=$(date +%s%N)" --overwrite
}

chainsaw_patch_status() {
  namespace="$1"
  resource="$2"
  name="$3"
  payload="$4"

  kubectl -n "$namespace" patch "$resource" "$name" \
    --subresource=status --type=merge -p "$payload" >/dev/null 2>&1 ||
    kubectl -n "$namespace" patch "$resource" "$name" \
      --type=merge -p "$payload" >/dev/null
}

chainsaw_rustfs_ready_payload() {
  tenant="$1"

  printf '{"status":{"availableReplicas":1,"currentState":"Ready","pools":[{"ssName":"%s-pool-0","state":"Ready"}],"conditions":[{"type":"Ready","status":"True","reason":"Ready","message":"Tenant is ready"}]}}' "$tenant"
}

chainsaw_cnpg_ready_payload() {
  printf '{"status":{"conditions":[{"type":"Ready","status":"True","reason":"ClusterReady","message":"Cluster is ready"}]}}'
}

chainsaw_authentik_debug() {
  resource="$1"
  name="$2"

  kubectl -n auth exec deployment/authentik-chainsaw -- python -c \
    'import sys, urllib.request; resource, name = sys.argv[1], sys.argv[2]; url = f"http://127.0.0.1:9000/debug/{resource}/{name}"; print(urllib.request.urlopen(url, timeout=10).read().decode(), end="")' \
    "$resource" "$name"
}

chainsaw_authentik_apply_count() {
  chainsaw_authentik_debug applies "$1" 2>/dev/null || printf '0'
}

chainsaw_wait_authentik_apply_count() {
  blueprint="$1"
  minimum="$2"
  timeout="$3"

  end=$(($(date +%s) + timeout))
  while true; do
    count="$(chainsaw_authentik_apply_count "$blueprint")"
    if [ "$count" -ge "$minimum" ]; then
      return 0
    fi
    if [ "$(date +%s)" -ge "$end" ]; then
      echo "timed out waiting for Authentik Blueprint $blueprint apply count >= $minimum" >&2
      return 1
    fi
    sleep 2
  done
}
