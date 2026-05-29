#!/usr/bin/env bash
set -euo pipefail

report_root="${1:-reports/chainsaw-logs}"
mkdir -p "${report_root}"

capture() {
  local name="$1"
  shift
  mkdir -p "$(dirname "${report_root}/${name}")"
  "$@" >"${report_root}/${name}.txt" 2>&1 || true
}

capture global/namespaces kubectl get namespaces
capture global/operator-pods kubectl -n tamoss-system get pods -o wide
capture global/operator-deploy kubectl -n tamoss-system describe deployment/operator-controller-manager
capture global/operator-logs kubectl -n tamoss-system logs deployment/operator-controller-manager -c manager --tail=-1
capture global/tamoss-all kubectl get tamoss --all-namespaces -o yaml
capture global/deployments-all kubectl get deployments --all-namespaces -o wide
capture global/jobs-all kubectl get jobs --all-namespaces -o wide
capture global/events-all kubectl get events --all-namespaces --sort-by=.lastTimestamp

mapfile -t chainsaw_namespaces < <(
  kubectl get namespaces \
    -l app.kubernetes.io/name=tamoss-chainsaw \
    -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' 2>/dev/null || true
)

for namespace in "${chainsaw_namespaces[@]}"; do
  [ -n "${namespace}" ] || continue

  bundle="namespaces/${namespace}"
  capture "${bundle}/resources" kubectl -n "${namespace}" get all,configmaps,tamoss,storagebackends,jobs -o wide
  capture "${bundle}/tamoss-describe" kubectl -n "${namespace}" describe tamoss
  capture "${bundle}/tamoss-yaml" kubectl -n "${namespace}" get tamoss -o yaml
  capture "${bundle}/storagebackends-describe" kubectl -n "${namespace}" describe storagebackends
  capture "${bundle}/deployments-describe" kubectl -n "${namespace}" describe deployments
  capture "${bundle}/pods-describe" kubectl -n "${namespace}" describe pods
  capture "${bundle}/jobs-describe" kubectl -n "${namespace}" describe jobs
  capture "${bundle}/events" kubectl -n "${namespace}" get events --sort-by=.lastTimestamp
done
