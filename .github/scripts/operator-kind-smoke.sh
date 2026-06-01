#!/usr/bin/env bash
set -euo pipefail

arch="${1:?usage: operator-kind-smoke.sh <amd64|arm64>}"
platform="linux/${arch}"
cluster="tamoss-operator-${arch}"
image="${OPERATOR_IMAGE:-livewyer/tamoss-operator:kind-smoke-${arch}}"
smoke_image="livewyer/tamoss-operator:kind-smoke"
node_image="${KIND_NODE_IMAGE:-kindest/node:v1.35.0}"
kubeconfig="${RUNNER_TEMP:-/tmp}/${cluster}.kubeconfig"
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

# shellcheck source=.tasks/lib/env.sh
. "${repo_root}/.tasks/lib/env.sh"
cd "${repo_root}"

cleanup() {
  kind delete cluster --name "${cluster}" >/dev/null 2>&1 || true
}
trap cleanup EXIT

docker buildx build \
  --build-arg VERSION=0.0.1 \
  --load \
  --platform "${platform}" \
  --tag "${image}" \
  --file operator/Dockerfile \
  .
docker tag "${image}" "${smoke_image}"

DOCKER_DEFAULT_PLATFORM="${platform}" kind create cluster \
  --image "${node_image}" \
  --kubeconfig "${kubeconfig}" \
  --name "${cluster}" \
  --wait 120s

kind load docker-image "${smoke_image}" --name "${cluster}"

platform_values="$(mktemp)"
cat > "${platform_values}" <<'EOF'
certManager:
  enabled: true
EOF
task_platform_helmfile \
  "${kubeconfig}" \
  deploy/platform/helmfile.yaml.gotmpl \
  "${platform_values}" \
  5m \
  sync \
  --selector component=cert-manager \
  --sync-args "--server-side=true --rollback-on-failure" \
  --wait \
  --wait-for-jobs
kubectl --kubeconfig "${kubeconfig}" wait \
  --for=condition=Established \
  crd/certificates.cert-manager.io \
  crd/issuers.cert-manager.io \
  --timeout=120s
kubectl --kubeconfig "${kubeconfig}" -n cert-manager rollout status \
  deployment/cert-manager \
  deployment/cert-manager-cainjector \
  deployment/cert-manager-webhook \
  --timeout=180s

KUBECONFIG="${kubeconfig}" kubectl apply --server-side -k operator/config/kind-smoke
KUBECONFIG="${kubeconfig}" kubectl wait \
  --for=condition=Established \
  crd/tamosses.tamoss.livewyer.io \
  --timeout=60s
KUBECONFIG="${kubeconfig}" kubectl -n tamoss-system rollout status \
  deployment/operator-controller-manager \
  --timeout=180s
KUBECONFIG="${kubeconfig}" kubectl -n tamoss-system get pods -o wide
