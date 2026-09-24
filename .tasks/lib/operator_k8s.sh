#!/usr/bin/env bash
set -euo pipefail

if [ -n "${BASH_SOURCE:-}" ]; then
  task_lib_dir="$(cd "$(dirname "${BASH_SOURCE}")" && pwd)"
else
  task_lib_dir="$(cd "${TASK_LIB_DIR:-.tasks/lib}" && pwd)"
fi
# shellcheck source=.tasks/lib/progress.sh
. "$task_lib_dir/progress.sh"

task_apply_operator() {
  local kubeconfig="$1"
  local operator_kustomize_dir="$2"

  task_step "Operator: apply TAMOSS operator" \
    kubectl --kubeconfig "$kubeconfig" apply --server-side -k "$operator_kustomize_dir"
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
  local defaults_revision="${5:-}"

  local version generation
  version="$(kubectl --kubeconfig "$kubeconfig" -n "$namespace" get "tamoss/$name" -o jsonpath='{.spec.version}')"
  generation="$(kubectl --kubeconfig "$kubeconfig" -n "$namespace" get "tamoss/$name" -o jsonpath='{.metadata.generation}')"
  if [ -z "$version" ]; then
    echo "Tamoss/$name requires spec.version before it can become ready." >&2
    return 1
  fi
  task_step "Instance: wait for Tamoss/$name generation $generation" \
    kubectl --kubeconfig "$kubeconfig" -n "$namespace" wait \
      --for="jsonpath={.status.conditions[?(@.type==\"Ready\")].observedGeneration}=$generation" \
      "tamoss/$name" --timeout="$timeout"
  task_step "Instance: wait for Tamoss/$name release $version" \
    kubectl --kubeconfig "$kubeconfig" -n "$namespace" wait \
      --for="jsonpath={.status.currentVersion}=$version" "tamoss/$name" --timeout="$timeout"
  if [ -n "$defaults_revision" ]; then
    task_step "Instance: wait for Tamoss/$name installation defaults" \
      kubectl --kubeconfig "$kubeconfig" -n "$namespace" wait \
        --for="jsonpath={.status.appliedDefaultsRevision}=$defaults_revision" "tamoss/$name" --timeout="$timeout"
  fi
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

# Render an isolated catalogue for local builds without changing checked-in defaults.
task_render_development_operator() {
  local source_dir="$1"
  local operand_tag="$2"
  local schema_version="$3"
  local previous_schema_version="$4"
  local output_dir="$5"
  local source_path
  mkdir -p "$output_dir"
  source_path="$(realpath --relative-to="$output_dir" "$source_dir")"
  python3 .github/scripts/release-catalogue.py --development \
    --operand-tag "$operand_tag" --schema-version "$schema_version" \
    --previous-schema-version "$previous_schema_version" \
    --output "$output_dir/catalogue.json"
  cat > "$output_dir/kustomization.yaml" <<YAML
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
- $source_path
configMapGenerator:
- name: operator-release-catalogue
  namespace: tamoss-system
  behavior: replace
  files:
  - catalogue.json
  options:
    immutable: true
YAML
}
