#!/usr/bin/env bash
set -euo pipefail

task_wait_authentik_platform() {
  local kubeconfig="$1"
  local namespace="$2"

  kubectl --kubeconfig "$kubeconfig" -n "$namespace" \
    rollout status statefulset/authentik-postgresql --timeout=10m
  kubectl --kubeconfig "$kubeconfig" -n "$namespace" \
    rollout status deployment/authentik-server --timeout=10m
  kubectl --kubeconfig "$kubeconfig" -n "$namespace" \
    rollout status deployment/authentik-worker --timeout=10m
}

task_wait_optional_backend_operators() {
  local kubeconfig="$1"
  local rustfs_namespace="$2"
  local rustfs_release="$3"
  local specs=(
    "clusters.postgresql.cnpg.io|cnpg-system|cnpg-controller-manager"
    "tenants.rustfs.com|$rustfs_namespace|$rustfs_release"
  )
  local spec
  local crd
  local namespace
  local deployment

  for spec in "${specs[@]}"; do
    IFS="|" read -r crd namespace deployment <<<"$spec"
    if ! kubectl --kubeconfig "$kubeconfig" get crd "$crd" >/dev/null 2>&1; then
      continue
    fi
    kubectl --kubeconfig "$kubeconfig" wait \
      --for=condition=Established "crd/$crd" --timeout=120s
    kubectl --kubeconfig "$kubeconfig" -n "$namespace" wait \
      --for=condition=Available "deployment/$deployment" --timeout=5m
  done
}
