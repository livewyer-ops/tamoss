#!/usr/bin/env bash
set -euo pipefail

if [ -n "${BASH_SOURCE:-}" ]; then
  task_lib_dir="$(cd "$(dirname "${BASH_SOURCE}")" && pwd)"
else
  task_lib_dir="$(cd "${TASK_LIB_DIR:-.tasks/lib}" && pwd)"
fi
# shellcheck source=.tasks/lib/progress.sh
. "$task_lib_dir/progress.sh"

# Content-derived tag for the operand images built from src/. A static tag leaves
# the Deployment spec unchanged after a rebuild, so Kubernetes has no reason to
# roll the pods and a local code change never reaches the cluster. Deriving the
# tag from source content makes the rendered spec differ whenever the code does.
# This triggers rollout.
# Anchored to the work tree root rather than the caller's directory: resolving
# src/ relative to the cwd would match nothing when called from elsewhere and
# return a constant tag, which is the staleness this function exists to prevent.
# Outside a work tree there is no content to hash, so "dev" is the honest answer
# and matches the tag used before content addressing.
task_kind_operand_tag() {
  local root paths

  root="$(git rev-parse --show-toplevel 2>/dev/null)" || root=""
  if [ -z "$root" ]; then
    printf 'dev'
    return 0
  fi

  paths="$(
    cd "$root" || exit 1
    git ls-files --cached --others --exclude-standard -- src \
      | sort -u \
      | while IFS= read -r path; do
          # An "if" rather than "&&": git lists the src/vendor/bbc-tams
          # submodule as a gitlink, which is not a regular file and sorts last,
          # so a short-circuiting test would leave the loop — and under
          # pipefail the whole assignment — with a non-zero status.
          if [ -f "$path" ]; then
            printf '%s\n' "$path"
          fi
        done
  )"

  if [ -z "$paths" ]; then
    printf 'task_kind_operand_tag: no operand sources found under %s/src\n' \
      "$root" >&2
    return 1
  fi

  printf 'dev-%s' "$(
    cd "$root" || exit 1
    {
      printf '%s\n' "$paths"
      printf '%s\n' "$paths" | git hash-object --stdin-paths
    } | sha256sum | cut -c1-12
  )"
}

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
