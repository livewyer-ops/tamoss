#!/usr/bin/env sh
set -eu

: "${NAMESPACE:?NAMESPACE is required}"

wait_for() {
  description="$1"
  shift
  end=$(($(date +%s) + 180))
  until "$@"; do
    if [ "$(date +%s)" -ge "$end" ]; then
      echo "timed out waiting for $description" >&2
      exit 1
    fi
    sleep 2
  done
}

has_api_secret() {
  kubectl -n "$NAMESPACE" get secret tamoss-api-token >/dev/null 2>&1
}

has_api_deployment() {
  kubectl -n "$NAMESPACE" get deployment tamoss-api >/dev/null 2>&1
}

secret_value() {
  kubectl -n "$NAMESPACE" get secret tamoss-api-token -o jsonpath='{.data.TAMOSS_API_TOKEN}'
}

checksum_value() {
  kubectl -n "$NAMESPACE" get deployment tamoss-api -o jsonpath='{.spec.template.metadata.annotations.checksum/api-token-secret}'
}

consumed_value() {
  kubectl -n "$NAMESPACE" get secret tamoss-api-token -o jsonpath='{.metadata.annotations.tamoss\.livewyer\.io/api-token-rotate-consumed}' 2>/dev/null || true
}

wait_for "api token secret" has_api_secret
wait_for "api deployment" has_api_deployment

before_secret="$(secret_value)"
before_checksum="$(checksum_value)"

kubectl -n "$NAMESPACE" annotate tamoss tamoss \
  tamoss.livewyer.io/api-token-rotate=rotate-1 --overwrite

end=$(($(date +%s) + 180))
while true; do
  after_secret="$(secret_value)"
  after_checksum="$(checksum_value)"
  consumed="$(consumed_value)"
  if [ "$consumed" = "rotate-1" ] && [ "$after_secret" != "$before_secret" ] && [ "$after_checksum" != "$before_checksum" ]; then
    break
  fi
  if [ "$(date +%s)" -ge "$end" ]; then
    kubectl -n "$NAMESPACE" get secret tamoss-api-token -o yaml
    kubectl -n "$NAMESPACE" get deployment tamoss-api -o yaml
    exit 1
  fi
  sleep 2
done
