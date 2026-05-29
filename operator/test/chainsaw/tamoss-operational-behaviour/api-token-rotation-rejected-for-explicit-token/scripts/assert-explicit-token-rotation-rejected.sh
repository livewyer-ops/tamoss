#!/usr/bin/env sh
set -eu

: "${NAMESPACE:?NAMESPACE is required}"

expected="dXNlci1zdXBwbGllZC10b2tlbg=="

end=$(($(date +%s) + 180))
while true; do
  actual="$(kubectl -n "$NAMESPACE" get secret tamoss-api-token -o jsonpath='{.data.TAMOSS_API_TOKEN}' 2>/dev/null || true)"
  if [ "$actual" = "$expected" ]; then
    break
  fi
  if [ "$(date +%s)" -ge "$end" ]; then
    kubectl -n "$NAMESPACE" get secret tamoss-api-token -o yaml 2>/dev/null || true
    exit 1
  fi
  sleep 2
done

consumed="$(kubectl -n "$NAMESPACE" get secret tamoss-api-token -o jsonpath='{.metadata.annotations.tamoss\.livewyer\.io/api-token-rotate-consumed}' 2>/dev/null || true)"
if [ -n "$consumed" ]; then
  kubectl -n "$NAMESPACE" get secret tamoss-api-token -o yaml
  exit 1
fi
