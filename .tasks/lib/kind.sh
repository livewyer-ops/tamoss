#!/usr/bin/env bash
set -euo pipefail

if [ -n "${BASH_SOURCE:-}" ]; then
  task_lib_dir="$(cd "$(dirname "${BASH_SOURCE}")" && pwd)"
else
  task_lib_dir="$(cd "${TASK_LIB_DIR:-.tasks/lib}" && pwd)"
fi
# shellcheck source=.tasks/lib/progress.sh
. "$task_lib_dir/progress.sh"

task_kind_ensure_cluster() {
  local project_name="$1"
  local kind_config="$2"
  local kubeconfig="$3"

  if kind get clusters | grep -Fxq "$project_name"; then
    kind export kubeconfig --name "$project_name" --kubeconfig "$kubeconfig"
    return
  fi
  kind create cluster --name "$project_name" --config "$kind_config" --kubeconfig "$kubeconfig"
}

task_kind_delete_cluster() {
  local project_name="$1"
  local kubeconfig="${2:-}"

  if [ -n "$kubeconfig" ]; then
    kind delete cluster --name "$project_name" --kubeconfig "$kubeconfig" || true
    return
  fi
  kind delete cluster --name "$project_name" || true
}

task_kind_load_image() {
  local project_name="$1"
  local label="$2"
  local image="$3"

  task_step "Local Kind: load $label image" \
    kind load docker-image "$image" --name "$project_name"
}

task_kind_build_image() {
  local label="$1"
  local image="$2"
  local dockerfile="$3"
  local context="$4"

  if [ -n "$dockerfile" ]; then
    task_step "Local Kind: build $label image" \
      docker build -t "$image" -f "$dockerfile" "$context"
    return
  fi

  task_step "Local Kind: build $label image" \
    docker build -t "$image" "$context"
}

task_kind_validate_profile_topology() {
  local profile="$1"
  local kubeconfig="$2"

  if [ "$profile" != "multi-server" ]; then
    return 0
  fi
  task_step "Local Kind: validate multi-node topology" \
    task_kind_assert_ready_node_count "$kubeconfig" 2
}

task_kind_assert_ready_node_count() {
  local kubeconfig="$1"
  local min_nodes="$2"
  local ready_count

  ready_count="$(
    kubectl --kubeconfig "$kubeconfig" get nodes --no-headers \
      | awk '$2 == "Ready" { ready += 1 } END { print ready + 0 }'
  )"
  if [ "$ready_count" -ge "$min_nodes" ]; then
    return 0
  fi

  echo "Expected at least $min_nodes Ready Kind nodes, found $ready_count." >&2
  echo "If this cluster was created for another profile, run task kind:down and retry." >&2
  kubectl --kubeconfig "$kubeconfig" get nodes >&2 || true
  return 1
}

task_remove_local_kind_state() {
  local kubeconfig="$1"
  local secrets_dir="$2"

  rm -rf "$kubeconfig" "$secrets_dir" || true
}
