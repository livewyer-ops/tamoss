#!/usr/bin/env bash
set -euo pipefail

if [ -n "${BASH_SOURCE:-}" ]; then
  task_lib_dir="$(cd "$(dirname "${BASH_SOURCE}")" && pwd)"
else
  task_lib_dir="$(cd "${TASK_LIB_DIR:-.tasks/lib}" && pwd)"
fi
# shellcheck source=.tasks/lib/progress.sh
. "$task_lib_dir/progress.sh"
# shellcheck source=.tasks/lib/operator_platform.sh
. "$task_lib_dir/operator_platform.sh"

task_apply_operator() {
  local kubeconfig="$1"
  local operator_kustomize_dir="$2"

  task_step "Operator: apply TAMOSS operator" \
    kubectl --kubeconfig "$kubeconfig" apply --server-side -k "$operator_kustomize_dir"
}

task_apply_operator_platform() {
  local kubeconfig="$1"
  local cert_manager_kustomize_dir="$2"
  local platform_kustomize_dir="$3"
  local authentik_namespace="$4"
  local rustfs_operator_namespace="$5"
  local rustfs_operator_release="$6"
  local platform_name

  platform_name="$(basename "$platform_kustomize_dir")"

  task_step "Platform: install cert-manager" \
    kubectl --kubeconfig "$kubeconfig" apply --server-side --force-conflicts -k "$cert_manager_kustomize_dir"
  task_step "Platform: wait for cert-manager" \
    kubectl --kubeconfig "$kubeconfig" -n cert-manager wait \
      --for=condition=Available \
      deployment/cert-manager deployment/cert-manager-cainjector deployment/cert-manager-webhook \
      --timeout=5m
  task_step "Platform: apply $platform_name services" \
    kubectl --kubeconfig "$kubeconfig" apply --server-side --force-conflicts -k "$platform_kustomize_dir"
  task_step "Platform: wait for Traefik" \
    kubectl --kubeconfig "$kubeconfig" -n traefik \
      rollout status deployment/traefik --timeout=3m
  task_step "Platform: wait for Authentik" \
    task_wait_authentik_platform "$kubeconfig" "$authentik_namespace"
  task_step "Platform: wait for CNPG and RustFS Operator" \
    task_wait_optional_backend_operators "$kubeconfig" "$rustfs_operator_namespace" "$rustfs_operator_release"
}

task_render_operator_kustomize() {
  local operator_kustomize_dir="$1"

  kubectl kustomize "$operator_kustomize_dir"
}

task_render_operator_monitoring() {
  kubectl kustomize operator/config/prometheus
}

task_wait_operator() {
  local kubeconfig="$1"
  local namespace="$2"
  local deployment="$3"

  task_step "Operator: wait for Tamoss CRD" \
    kubectl --kubeconfig "$kubeconfig" wait \
      --for=condition=Established \
      crd/tamosses.tamoss.livewyer.io \
      --timeout=60s
  task_step "Operator: wait for TAMOSS operator" \
    kubectl --kubeconfig "$kubeconfig" -n "$namespace" \
      rollout status "deploy/$deployment" --timeout=5m
}

task_restart_operator() {
  local kubeconfig="$1"
  local namespace="$2"
  local deployment="$3"

  task_step "Operator: restart TAMOSS operator" \
    kubectl --kubeconfig "$kubeconfig" -n "$namespace" \
      rollout restart "deploy/$deployment"
  task_step "Operator: wait for TAMOSS operator" \
    kubectl --kubeconfig "$kubeconfig" -n "$namespace" \
      rollout status "deploy/$deployment" --timeout=5m
}

task_uninstall_operator() {
  local kubeconfig="$1"
  local operator_kustomize_dir="$2"

  task_step "Operator: delete TAMOSS operator" \
    kubectl --kubeconfig "$kubeconfig" delete -k "$operator_kustomize_dir" --ignore-not-found
}

task_apply_tamoss_instance() {
  local kubeconfig="$1"
  local tamoss_cr="$2"

  if [ -d "$tamoss_cr" ]; then
    task_step "Instance: apply Tamoss profile" \
      kubectl --kubeconfig "$kubeconfig" apply -k "$tamoss_cr"
    return
  fi
  task_step "Instance: apply Tamoss manifest" \
    kubectl --kubeconfig "$kubeconfig" apply -f "$tamoss_cr"
}

task_wait_tamoss_instance() {
  local kubeconfig="$1"
  local namespace="$2"
  local name="$3"
  local timeout="$4"

  task_step "Instance: wait for Tamoss/$name Ready" \
    kubectl --kubeconfig "$kubeconfig" -n "$namespace" wait \
      --for=condition=Ready "tamoss/$name" --timeout="$timeout"
}

task_delete_tamoss_instance() {
  local kubeconfig="$1"
  local tamoss_cr="$2"
  local namespace="$3"
  local name="$4"

  if [ -d "$tamoss_cr" ]; then
    task_step "Instance: delete Tamoss instance" \
      kubectl --kubeconfig "$kubeconfig" delete -k "$tamoss_cr" --ignore-not-found
    return
  fi
  if [ -f "$tamoss_cr" ]; then
    task_step "Instance: delete Tamoss manifest" \
      kubectl --kubeconfig "$kubeconfig" delete -f "$tamoss_cr" --ignore-not-found
    return
  fi
  task_step "Instance: delete Tamoss/$name" \
    kubectl --kubeconfig "$kubeconfig" -n "$namespace" \
      delete tamoss "$name" --ignore-not-found
}
