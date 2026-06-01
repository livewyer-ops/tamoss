#!/usr/bin/env bash
set -euo pipefail

if [ -n "${BASH_SOURCE:-}" ]; then
  task_lib_dir="$(cd "$(dirname "${BASH_SOURCE}")" && pwd)"
else
  task_lib_dir="$(cd "${TASK_LIB_DIR:-.tasks/lib}" && pwd)"
fi
# shellcheck source=.tasks/lib/progress.sh
. "$task_lib_dir/progress.sh"

task_operator_chainsaw_up() {
  local cluster="${CHAINSAW_CLUSTER_NAME:-tamoss-chainsaw}"
  local kubeconfig="${KUBECONFIG:-.local/chainsaw.kubeconfig}"
  local operator_image="${OPERATOR_IMAGE:-livewyer/tamoss-operator:chainsaw}"
  local chainsaw_image="livewyer/tamoss-operator:chainsaw"
  local api_image="${API_IMAGE:-livewyer/tamoss-api:dev}"
  local keep_cluster="${CHAINSAW_KEEP_CLUSTER:-false}"
  local version="${VERSION:-0.0.1}"
  local install_rustfs_operator="${CHAINSAW_INSTALL_RUSTFS_OPERATOR:-false}"
  local install_cnpg_operator="${CHAINSAW_INSTALL_CNPG_OPERATOR:-false}"
  local install_authentik_fixture="${CHAINSAW_INSTALL_AUTHENTIK_FIXTURE:-false}"
  local platform_chart_dir="${CHAINSAW_PLATFORM_CHART_DIR:-deploy/platform/chart}"

  mkdir -p "$(dirname "$kubeconfig")" reports

  task_operator_chainsaw_register_cleanup "$cluster" "$keep_cluster"

  task_step "Operator Chainsaw: build operator image" \
    make -C operator docker-build IMG="$operator_image" SCHEMA_VERSION="$version"
  docker tag "$operator_image" "$chainsaw_image"

  task_step "Operator Chainsaw: build API image" \
    docker build -t "$api_image" -f src/app/tamoss/Dockerfile .

  kind delete cluster --name "$cluster" >/dev/null 2>&1 || true
  task_step "Operator Chainsaw: create Kind cluster" \
    kind create cluster \
      --name "$cluster" \
      --kubeconfig "$kubeconfig" \
      --config operator/test/chainsaw/kind-config.yaml \
      --wait 120s

  task_step "Operator Chainsaw: load operator image" \
    kind load docker-image "$chainsaw_image" --name "$cluster"
  task_step "Operator Chainsaw: load API image" \
    kind load docker-image "$api_image" --name "$cluster"

  task_operator_chainsaw_apply_platform_prerequisites \
    "$kubeconfig" \
    "$platform_chart_dir" \
    "$install_rustfs_operator" \
    "$install_cnpg_operator"
  task_operator_chainsaw_apply_operator "$kubeconfig"

  if [ "$install_authentik_fixture" = "true" ]; then
    kubectl --kubeconfig "$kubeconfig" apply --server-side -f operator/test/chainsaw/fixtures/authentik.yaml
    kubectl --kubeconfig "$kubeconfig" -n auth rollout status deployment/authentik-chainsaw --timeout=180s
  fi

  KUBECONFIG="$kubeconfig" task operator:e2e:chainsaw \
    KUBECONFIG="$kubeconfig" \
    REPORT_PATH="${CHAINSAW_REPORT_PATH:-reports}" \
    CHAINSAW_SELECTOR="${CHAINSAW_SELECTOR:-}" \
    CHAINSAW_EXTRA_ARGS="${CHAINSAW_EXTRA_ARGS:-}" \
    ${CHAINSAW_TASK_ARGS:-}
}

task_operator_chainsaw_register_cleanup() {
  local cluster="$1"
  local keep_cluster="$2"

  if [ "$keep_cluster" = "true" ]; then
    return
  fi
  TASK_OPERATOR_CHAINSAW_CLEANUP_CLUSTER="$cluster"
  export TASK_OPERATOR_CHAINSAW_CLEANUP_CLUSTER
  trap 'kind delete cluster --name "$TASK_OPERATOR_CHAINSAW_CLEANUP_CLUSTER" >/dev/null 2>&1 || true' EXIT
}

task_operator_chainsaw_apply_platform_prerequisites() {
  local kubeconfig="$1"
  local platform_chart_dir="$2"
  local install_rustfs_operator="$3"
  local install_cnpg_operator="$4"

  task_step "Operator Chainsaw: build platform chart dependencies" \
    helm dependency build "$platform_chart_dir"

  task_step "Operator Chainsaw: apply platform prerequisites" \
    sh -c 'helm --kubeconfig "$1" template tamoss-platform "$2" --namespace tamoss-platform --set certManager.enabled=true --set cnpg.enabled="$3" --set rustfsOperator.enabled="$4" --set authentik.enabled=false --set traefik.enabled=false --set traefikCrds.enabled=false | kubectl --kubeconfig "$1" apply --server-side --force-conflicts -f -' \
      sh "$kubeconfig" "$platform_chart_dir" "$install_cnpg_operator" "$install_rustfs_operator"

  kubectl --kubeconfig "$kubeconfig" wait \
    --for=condition=Established \
    crd/certificates.cert-manager.io \
    crd/issuers.cert-manager.io \
    --timeout=120s
  kubectl --kubeconfig "$kubeconfig" -n cert-manager rollout status \
    deployment/cert-manager \
    deployment/cert-manager-cainjector \
    deployment/cert-manager-webhook \
    --timeout=180s

  if [ "$install_cnpg_operator" = "true" ]; then
    kubectl --kubeconfig "$kubeconfig" wait \
      --for=condition=Established \
      crd/clusters.postgresql.cnpg.io \
      --timeout=120s
    kubectl --kubeconfig "$kubeconfig" -n cnpg-system rollout status \
      deployment/cnpg-controller-manager \
      --timeout=180s
  fi

  if [ "$install_rustfs_operator" = "true" ]; then
    kubectl --kubeconfig "$kubeconfig" wait \
      --for=condition=Established \
      crd/tenants.rustfs.com \
      --timeout=120s
    kubectl --kubeconfig "$kubeconfig" -n rustfs-system rollout status \
      deployment/rustfs-operator \
      --timeout=180s
  fi
}

task_operator_chainsaw_apply_operator() {
  local kubeconfig="$1"

  kubectl --kubeconfig "$kubeconfig" apply --server-side -f operator/test/chainsaw/fixtures/gateway-api-crds.yaml
  kubectl --kubeconfig "$kubeconfig" apply --server-side -k operator/config/chainsaw
  kubectl --kubeconfig "$kubeconfig" wait \
    --for=condition=Established \
    crd/tamosses.tamoss.livewyer.io \
    --timeout=60s
  kubectl --kubeconfig "$kubeconfig" -n tamoss-system rollout status \
    deployment/operator-controller-manager \
    --timeout=180s
}
