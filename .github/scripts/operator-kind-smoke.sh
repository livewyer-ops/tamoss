#!/usr/bin/env bash
set -euo pipefail

arch="${1:?usage: operator-kind-smoke.sh <amd64|arm64>}"
platform="linux/${arch}"
cluster="tamoss-operator-${arch}"
image="${OPERATOR_IMAGE:-livewyer/tamoss-operator:kind-smoke-${arch}}"
smoke_image="livewyer/tamoss-operator:kind-smoke"
node_image="${KIND_NODE_IMAGE:-kindest/node:v1.35.0}"
kubeconfig="${RUNNER_TEMP:-/tmp}/${cluster}.kubeconfig"

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

helm dependency build deploy/platform/chart
helm --kubeconfig "${kubeconfig}" template tamoss-platform deploy/platform/chart \
  --namespace tamoss-platform \
  --set certManager.enabled=true \
  --set cnpg.enabled=false \
  --set rustfsOperator.enabled=false \
  --set authentik.enabled=false \
  --set traefik.enabled=false \
  --set traefikCrds.enabled=false |
  kubectl --kubeconfig "${kubeconfig}" apply --server-side --force-conflicts -f -
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
