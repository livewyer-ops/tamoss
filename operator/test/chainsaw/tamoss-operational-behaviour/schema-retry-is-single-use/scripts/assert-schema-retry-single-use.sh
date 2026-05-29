#!/usr/bin/env sh
set -eu

: "${NAMESPACE:?NAMESPACE is required}"

state_value() {
  kubectl -n "$NAMESPACE" get configmap tamoss-schema-state -o "jsonpath={$1}" 2>/dev/null || true
}

consumed_retry() {
  kubectl -n "$NAMESPACE" get configmap tamoss-schema-state -o jsonpath='{.metadata.annotations.tamoss\.livewyer\.io/schema-retry-consumed}' 2>/dev/null || true
}

kubectl -n "$NAMESPACE" annotate tamoss tamoss \
  tamoss.livewyer.io/schema-retry=retry-once --overwrite

end=$(($(date +%s) + 300))
while true; do
  consumed="$(consumed_retry)"
  count="$(state_value '.data.failureCount')"
  if [ "$consumed" = "retry-once" ] && [ "$count" = "3" ]; then
    break
  fi
  if [ "$(date +%s)" -ge "$end" ]; then
    kubectl -n "$NAMESPACE" get tamoss tamoss -o yaml
    kubectl -n "$NAMESPACE" get configmap tamoss-schema-state -o yaml
    exit 1
  fi
  sleep 2
done

before_job="$(state_value '.data.failedJobUID')"
kubectl -n "$NAMESPACE" annotate tamoss tamoss \
  tamoss.livewyer.io/schema-retry=retry-once --overwrite
# Negative assertion window: duplicate retry consumption has no positive
# Kubernetes condition to wait on, so this loop samples state for one short
# controller-reconcile window and fails immediately if a duplicate retry changes
# failure state.
end=$(($(date +%s) + 30))
while true; do
  after_count="$(state_value '.data.failureCount')"
  after_job="$(state_value '.data.failedJobUID')"
  after_consumed="$(consumed_retry)"
  if [ "$after_count" != "3" ] || [ "$after_job" != "$before_job" ] || [ "$after_consumed" != "retry-once" ]; then
    kubectl -n "$NAMESPACE" get configmap tamoss-schema-state -o yaml
    exit 1
  fi
  if [ "$(date +%s)" -ge "$end" ]; then
    break
  fi
  sleep 2
done
