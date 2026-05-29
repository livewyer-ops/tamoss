#!/usr/bin/env bash
set -euo pipefail

if [ -n "${BASH_SOURCE:-}" ]; then
  task_lib_dir="$(cd "$(dirname "${BASH_SOURCE}")" && pwd)"
else
  task_lib_dir="$(cd "${TASK_LIB_DIR:-.tasks/lib}" && pwd)"
fi
# shellcheck source=.tasks/lib/progress.sh
. "$task_lib_dir/progress.sh"

task_operator_upgrade_export_ref() {
  local ref="$1"
  local destination="$2"
  git archive "$ref" | tar -x -C "$destination"
}

task_operator_upgrade_previous_image() {
  printf '%s\n' "${TAMOSS_OPERATOR_PREVIOUS_IMAGE:-livewyer/tamoss-operator:upgrade-previous}"
}

task_operator_upgrade_previous_config_dir() {
  printf '%s\n' "${TAMOSS_OPERATOR_PREVIOUS_CONFIG_DIR:-${TAMOSS_OPERATOR_UPGRADE_WORKDIR:-.local/operator-upgrade}/previous-source/operator/config/default}"
}

task_operator_upgrade_prepare_previous() {
  local previous_ref="${TAMOSS_OPERATOR_PREVIOUS_REF:-HEAD^}"
  local previous_image
  local workdir="${TAMOSS_OPERATOR_UPGRADE_WORKDIR:-.local/operator-upgrade}"
  local previous_source="$workdir/previous-source"
  local previous_config_dir
  previous_config_dir="$(task_operator_upgrade_previous_config_dir)"

  previous_image="$(task_operator_upgrade_previous_image)"

  if [ -n "${TAMOSS_OPERATOR_PREVIOUS_IMAGE:-}" ] && [ -d "$previous_config_dir" ]; then
    return 0
  fi

  rm -rf "$previous_source"
  mkdir -p "$previous_source"
  task_step "Operator upgrade: export previous source" \
    task_operator_upgrade_export_ref "$previous_ref" "$previous_source"
  if [ -z "${TAMOSS_OPERATOR_PREVIOUS_IMAGE:-}" ]; then
    task_step "Operator upgrade: build previous operator image" \
      docker build \
        --build-arg VERSION="${TAMOSS_OPERATOR_PREVIOUS_VERSION:-0.0.1}" \
        -t "$previous_image" \
        -f "$previous_source/operator/Dockerfile" \
        "$previous_source"
  fi
}

task_operator_upgrade_apply_operator() {
  local kubeconfig="$1"
  local config_dir="$2"
  local image="$3"
  local repository tag overlay config_ref rendered

  repository="${image%:*}"
  tag="${image##*:}"
  if [ "$repository" = "$image" ] || [ -z "$repository" ] || [ -z "$tag" ] || printf '%s' "$tag" | grep -q '/'; then
    echo "operator image must include an explicit repository:tag: $image" >&2
    return 2
  fi

  config_dir="$(cd "$config_dir" && pwd)"
  overlay="$(mktemp -d)"
  config_ref="$(python3 -c 'import os, sys; print(os.path.relpath(sys.argv[1], sys.argv[2]))' "$config_dir" "$overlay")"
  cat > "$overlay/kustomization.yaml" <<EOF
resources:
  - $config_ref
images:
  - name: livewyer/tamoss-operator
    newName: $repository
    newTag: $tag
EOF
  rendered="$(kubectl kustomize --load-restrictor=LoadRestrictionsNone "$overlay")"
  printf '%s\n' "$rendered" | kubectl --kubeconfig "$kubeconfig" apply --server-side -f -
  kubectl --kubeconfig "$kubeconfig" -n tamoss-system rollout status \
    deployment/operator-controller-manager \
    --timeout=5m
  rm -rf "$overlay"
}

task_operator_upgrade_validate_existing_cluster() {
  local kubeconfig="$1"
  local current_image="$2"
  local tamoss_namespace="$3"
  local tamoss_name="$4"
  local current_config_dir="${TAMOSS_OPERATOR_CURRENT_CONFIG_DIR:-operator/config/default}"

  mkdir -p reports
  task_step "Operator upgrade: validate in-place upgrade" \
    env \
      KUBECONFIG="$kubeconfig" \
      TAMOSS_OPERATOR_CURRENT_IMAGE="$current_image" \
      TAMOSS_OPERATOR_CURRENT_CONFIG_DIR="$current_config_dir" \
      TAMOSS_NAMESPACE="$tamoss_namespace" \
      TAMOSS_NAME="$tamoss_name" \
      uv run --project src pytest -c src/pyproject.toml \
        tests/e2e/test_operator_upgrade.py \
        -m operator_upgrade \
        -ra \
        --junitxml=reports/junit-e2e-operator-upgrade.xml
}
