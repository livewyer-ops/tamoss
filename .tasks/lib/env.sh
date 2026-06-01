#!/usr/bin/env bash
set -euo pipefail

if [ -n "${BASH_SOURCE:-}" ]; then
  task_lib_dir="$(cd "$(dirname "${BASH_SOURCE}")" && pwd)"
else
  task_lib_dir="$(cd "${TASK_LIB_DIR:-.tasks/lib}" && pwd)"
fi
# shellcheck source=.tasks/lib/progress.sh
. "$task_lib_dir/progress.sh"
# shellcheck source=.tasks/lib/operator_k8s.sh
. "$task_lib_dir/operator_k8s.sh"
# shellcheck source=.tasks/lib/profile.sh
. "$task_lib_dir/profile.sh"

task_tamoss_field_from_rendered() {
  local rendered="$1"
  local field="$2"
  local query

  case "$field" in
    baseDomain) query='.spec.publicEndpoint.baseDomain // ""' ;;
    profile) query='.spec.profile // ""' ;;
    namespace) query='.metadata.namespace // ""' ;;
    name) query='.metadata.name // ""' ;;
    *)
      echo "Unsupported rendered Tamoss field: $field" >&2
      return 2
      ;;
  esac

  yq -r "select(.kind == \"Tamoss\") | ${query}" "$rendered" \
    | sed '/^---$/d; /^$/d' \
    | sed -n '1p'
}

task_render_environment() {
  local environment_dir="$1"
  local rendered="$2"
  local missing_message="$3"

  if [ ! -d "$environment_dir" ]; then
    echo "$missing_message" >&2
    return 1
  fi
  kubectl kustomize "$environment_dir" > "$rendered"
}

task_k8s_secret_value() {
  local kubeconfig="$1"
  local namespace="$2"
  local secret="$3"
  local key="$4"

  if [ -z "$secret" ] || [ -z "$key" ]; then
    return 0
  fi
  kubectl --kubeconfig "$kubeconfig" \
    -n "$namespace" \
    get secret "$secret" \
    -o "jsonpath={.data.${key}}" 2>/dev/null \
    | base64 --decode || true
}

task_k8s_resource_value() {
  local kubeconfig="$1"
  local namespace="$2"
  local resource="$3"
  local jsonpath="$4"

  kubectl --kubeconfig "$kubeconfig" \
    -n "$namespace" \
    get "$resource" \
    -o "jsonpath=${jsonpath}" 2>/dev/null || true
}

task_k8s_secret_first_value() {
  local kubeconfig="$1"
  local namespace="$2"
  local secret="$3"
  shift 3
  local key value

  for key in "$@"; do
    value="$(task_k8s_secret_value "$kubeconfig" "$namespace" "$secret" "$key")"
    if [ -n "$value" ]; then
      printf '%s\n' "$value"
      return 0
    fi
  done
}

task_init_env() {
  local name="$1"
  local profile="$2"
  local domain="$3"
  local template_dir="deploy/templates/environment"
  local environment_dir="deploy/environments/$name"

  case "$name" in
    *[!a-z0-9-]*|""|-*)
      echo "NAME must be a non-empty DNS-style name using lowercase letters, numbers, and hyphens." >&2
      return 2
      ;;
  esac
  case "$profile" in
    single-server|multi-server) ;;
    *)
      echo "PROFILE must be single-server or multi-server for remote Kubernetes environments." >&2
      return 2
      ;;
  esac
  if [ ! -d "$template_dir" ]; then
    echo "Environment template directory $template_dir was not found." >&2
    return 1
  fi
  if [ -e "$environment_dir" ]; then
    echo "Environment $environment_dir already exists; refusing to overwrite it." >&2
    return 1
  fi

  mkdir -p "$(dirname "$environment_dir")"
  cp -R "$template_dir" "$environment_dir"
  for file in "$environment_dir"/*.yaml; do
    tmp_file="$(mktemp)"
    sed \
      -e "s|__PROFILE__|$profile|g" \
      -e "s|__DOMAIN__|$domain|g" \
      "$file" > "$tmp_file"
    mv "$tmp_file" "$file"
  done

  printf 'Created %s\n' "$environment_dir"
  printf 'Edit the YAML files there, then run:\n'
  printf '  task env:apply ENV=%s KUBECONFIG=/path/to/kubeconfig\n' "$name"
}

task_env_values_enabled() {
  local values_file="$1"
  local query="$2"

  [ "$(task_platform_values_query "$values_file" "${query} // false")" = "true" ]
}

task_platform_values_query() {
  local values_file="$1"
  local query="$2"

  yq ea -r "select(fileIndex == 0) * select(fileIndex == 1) | ${query}" \
    deploy/platform/values/defaults.yaml \
    "$values_file"
}

task_platform_values_string() {
  local values_file="$1"
  local query="$2"
  local fallback="$3"
  local value

  value="$(task_platform_values_query "$values_file" "${query} // \"\"")"
  printf '%s\n' "${value:-$fallback}"
}

task_timeout_to_seconds() {
  local timeout="$1"

  case "$timeout" in
    *m) printf '%s\n' "$((${timeout%m} * 60))" ;;
    *s) printf '%s\n' "${timeout%s}" ;;
    *[!0-9]*|"")
      echo "Unsupported timeout format $timeout; use seconds, Ns, or Nm." >&2
      return 2
      ;;
    *) printf '%s\n' "$timeout" ;;
  esac
}

task_platform_helmfile() {
  local kubeconfig="$1"
  local helmfile_path="$2"
  local values_file="$3"
  local timeout="$4"
  shift 4
  local timeout_seconds
  local cwd kubeconfig_abs values_abs helmfile_dir helmfile_name

  cwd="$(pwd)"
  case "$kubeconfig" in
    /*) kubeconfig_abs="$kubeconfig" ;;
    *) kubeconfig_abs="$cwd/$kubeconfig" ;;
  esac
  values_abs="$(cd "$(dirname "$values_file")" && pwd)/$(basename "$values_file")"
  helmfile_dir="$(cd "$(dirname "$helmfile_path")" && pwd)"
  helmfile_name="$(basename "$helmfile_path")"

  timeout_seconds="$(task_timeout_to_seconds "$timeout")"
  (
    cd "$helmfile_dir"
    helmfile \
      --kubeconfig "$kubeconfig_abs" \
      --file "$helmfile_name" \
      --state-values-file values/defaults.yaml \
      --state-values-file "$values_abs" \
      --state-values-set "platform.timeoutSeconds=${timeout_seconds}" \
      "$@"
  )
}

task_diff_platform_helmfile() {
  local kubeconfig="$1"
  local helmfile_path="$2"
  local values_file="$3"
  local timeout="$4"

  task_platform_helmfile "$kubeconfig" "$helmfile_path" "$values_file" "$timeout" template --include-crds \
    | kubectl --kubeconfig "$kubeconfig" diff -f - || true
}

task_apply_env_platform() {
  local kubeconfig="$1"
  local environment_dir="$2"
  local helmfile_path="$3"
  local timeout="$4"
  local values_file="$environment_dir/platform-values.yaml"

  if [ ! -f "$values_file" ]; then
    echo "Environment platform values $values_file were not found." >&2
    return 1
  fi
  if [ ! -f "$helmfile_path" ]; then
    echo "Platform Helmfile $helmfile_path was not found." >&2
    return 1
  fi

  task_step "Platform: apply Helmfile releases" \
    task_platform_helmfile "$kubeconfig" "$helmfile_path" "$values_file" "$timeout" \
      sync \
      --sync-args "--server-side=true --rollback-on-failure" \
      --wait \
      --wait-for-jobs

  task_wait_env_platform "$kubeconfig" "$values_file" "$timeout"
}

task_delete_env_platform() {
  local kubeconfig="$1"
  local environment_dir="$2"
  local helmfile_path="$3"
  local timeout="$4"
  local values_file="$environment_dir/platform-values.yaml"

  if [ ! -d "$environment_dir" ]; then
    echo "Environment directory $environment_dir was not found." >&2
    return 1
  fi
  if [ ! -f "$values_file" ]; then
    echo "Environment platform values $values_file were not found." >&2
    return 1
  fi
  if [ ! -f "$helmfile_path" ]; then
    echo "Platform Helmfile $helmfile_path was not found." >&2
    return 1
  fi

  task_step "Platform: destroy Helmfile releases" \
    task_platform_helmfile "$kubeconfig" "$helmfile_path" "$values_file" "$timeout" \
      destroy \
      --deleteWait \
      --cascade foreground
}

task_wait_authentik_platform() {
  local kubeconfig="$1"
  local namespace="$2"

  kubectl --kubeconfig "$kubeconfig" -n "$namespace" \
    rollout status statefulset/authentik-postgresql --timeout=10m
  kubectl --kubeconfig "$kubeconfig" -n "$namespace" \
    rollout status deployment/authentik-server --timeout=10m
  kubectl --kubeconfig "$kubeconfig" -n "$namespace" \
    rollout status deployment/authentik-worker --timeout=10m
}

task_wait_env_platform() {
  local kubeconfig="$1"
  local values_file="$2"
  local timeout="$3"
  local cert_manager_namespace traefik_namespace auth_namespace cnpg_namespace rustfs_namespace

  cert_manager_namespace="$(task_platform_values_string "$values_file" '."cert-manager".namespace' cert-manager)"
  traefik_namespace="$(task_platform_values_string "$values_file" '.traefik.namespaceOverride' traefik)"
  auth_namespace="$(task_platform_values_string "$values_file" '.authentikChart.namespaceOverride // .authentikChart.global.namespaceOverride' auth)"
  cnpg_namespace="$(task_platform_values_string "$values_file" '.cnpg.namespaceOverride' cnpg-system)"
  rustfs_namespace="$(task_platform_values_string "$values_file" '.rustfsOperator.namespace' rustfs-system)"

  if task_env_values_enabled "$values_file" ".certManager.enabled"; then
    task_step "Platform: wait for cert-manager" \
      kubectl --kubeconfig "$kubeconfig" -n "$cert_manager_namespace" wait \
        --for=condition=Available \
        deployment/cert-manager deployment/cert-manager-cainjector deployment/cert-manager-webhook \
        --timeout="$timeout"
  fi
  if task_env_values_enabled "$values_file" ".traefik.enabled"; then
    task_step "Platform: wait for Traefik" \
      kubectl --kubeconfig "$kubeconfig" -n "$traefik_namespace" \
        rollout status deployment/traefik --timeout="$timeout"
  fi
  if task_env_values_enabled "$values_file" ".authentik.enabled"; then
    task_step "Platform: wait for Authentik" \
      task_wait_authentik_platform "$kubeconfig" "$auth_namespace"
  fi
  if task_env_values_enabled "$values_file" ".cnpg.enabled"; then
    task_step "Platform: wait for CNPG" \
      task_wait_platform_deployment "$kubeconfig" clusters.postgresql.cnpg.io "$cnpg_namespace" cnpg-controller-manager "$timeout"
  fi
  if task_env_values_enabled "$values_file" ".rustfsOperator.enabled"; then
    task_step "Platform: wait for RustFS Operator" \
      task_wait_platform_deployment "$kubeconfig" tenants.rustfs.com "$rustfs_namespace" rustfs-operator "$timeout"
  fi
}

task_wait_platform_deployment() {
  local kubeconfig="$1"
  local crd="$2"
  local namespace="$3"
  local deployment="$4"
  local timeout="$5"

  kubectl --kubeconfig "$kubeconfig" wait \
    --for=condition=Established "crd/$crd" --timeout="$timeout"
  kubectl --kubeconfig "$kubeconfig" -n "$namespace" wait \
    --for=condition=Available "deployment/$deployment" --timeout="$timeout"
}

task_apply_env() {
  local environment_dir="$1"
  local kubeconfig="$2"
  local helmfile_path="$3"
  local operator_kustomize_dir="$4"
  local platform_timeout="$5"
  local rendered profile namespace name env_name

  env_name="$(basename "$environment_dir")"
  rendered="$(mktemp)"
  trap "rm -f '$rendered'" EXIT

  task_render_environment \
    "$environment_dir" \
    "$rendered" \
    "Environment $environment_dir was not found. Create it with task env:init."

  profile="$(task_tamoss_field_from_rendered "$rendered" profile)"
  namespace="$(task_tamoss_field_from_rendered "$rendered" namespace)"
  name="$(task_tamoss_field_from_rendered "$rendered" name)"
  namespace="${namespace:-tams}"

  case "$profile" in
    local-kind|single-server|multi-server) ;;
    *)
      echo "Unable to infer a supported Tamoss spec.profile from $environment_dir." >&2
      return 1
      ;;
  esac
  if [ -z "$name" ]; then
    echo "Unable to infer Tamoss metadata.name from $environment_dir." >&2
    return 1
  fi

  task_apply_env_platform \
    "$kubeconfig" \
    "$environment_dir" \
    "$helmfile_path" \
    "$platform_timeout"
  task_apply_operator "$kubeconfig" "$operator_kustomize_dir"
  task_wait_operator "$kubeconfig" tamoss-system operator-controller-manager
  task_apply_env_instance "$environment_dir" "$kubeconfig"

  printf 'Applied %s/%s from %s\n' "$namespace" "$name" "$environment_dir"
}

task_apply_env_instance() {
  local environment_dir="$1"
  local kubeconfig="$2"
  local env_name

  env_name="$(basename "$environment_dir")"
  task_step "Instance: apply TAMOSS environment $env_name" \
    kubectl --kubeconfig "$kubeconfig" apply -k "$environment_dir"
}

task_wait_env() {
  local environment_dir="$1"
  local kubeconfig="$2"
  local timeout="$3"
  local rendered namespace name

  rendered="$(mktemp)"
  trap "rm -f '$rendered'" EXIT

  task_render_environment \
    "$environment_dir" \
    "$rendered" \
    "Environment $environment_dir was not found. Create it with task env:init."
  namespace="$(task_tamoss_field_from_rendered "$rendered" namespace)"
  name="$(task_tamoss_field_from_rendered "$rendered" name)"
  namespace="${namespace:-tams}"
  if [ -z "$name" ]; then
    echo "Unable to infer Tamoss metadata.name from $environment_dir." >&2
    return 1
  fi

  task_step "wait for Tamoss instance" \
    kubectl --kubeconfig "$kubeconfig" -n "$namespace" \
      wait --for=condition=Ready "tamoss/$name" --timeout="$timeout"
}

task_show_env_status() {
  local environment_dir="$1"
  local kubeconfig="$2"
  local rendered namespace name

  rendered="$(mktemp)"
  trap "rm -f '$rendered'" EXIT

  task_render_environment \
    "$environment_dir" \
    "$rendered" \
    "Environment $environment_dir was not found. Create it with task env:init."
  namespace="$(task_tamoss_field_from_rendered "$rendered" namespace)"
  name="$(task_tamoss_field_from_rendered "$rendered" name)"
  namespace="${namespace:-tams}"
  if [ -z "$name" ]; then
    echo "Unable to infer Tamoss metadata.name from $environment_dir." >&2
    return 1
  fi

  kubectl --kubeconfig "$kubeconfig" -n "$namespace" get "tamoss/$name" -o wide
  kubectl --kubeconfig "$kubeconfig" -n "$namespace" get pods,svc,ingress
  if kubectl --kubeconfig "$kubeconfig" api-resources --api-group=gateway.networking.k8s.io | grep -q '^httproutes'; then
    kubectl --kubeconfig "$kubeconfig" -n "$namespace" get httproute
  fi
  kubectl --kubeconfig "$kubeconfig" -n "$namespace" get events --sort-by=.lastTimestamp | tail -40
}

task_diff_env() {
  local environment_dir="$1"
  local kubeconfig="$2"
  local helmfile_path="$3"
  local platform_timeout="$4"
  local operator_kustomize_dir="$5"
  local values_file="$environment_dir/platform-values.yaml"

  if [ ! -f "$values_file" ]; then
    echo "Environment platform values $values_file were not found." >&2
    return 1
  fi
  if [ ! -f "$helmfile_path" ]; then
    echo "Platform Helmfile $helmfile_path was not found." >&2
    return 1
  fi

  task_step "Platform: diff Helmfile releases" \
    task_diff_platform_helmfile "$kubeconfig" "$helmfile_path" "$values_file" "$platform_timeout"
  task_step "Operator: diff TAMOSS operator" \
    kubectl --kubeconfig "$kubeconfig" diff --server-side -k "$operator_kustomize_dir" || true
  task_step "Instance: diff TAMOSS environment" \
    kubectl --kubeconfig "$kubeconfig" diff -k "$environment_dir" || true
}

task_condition_summary() {
  local kubeconfig="$1"
  local namespace="$2"
  local label="$3"
  local resource="$4"
  local name="$5"
  local condition="$6"
  local value
  local status_reason suffix

  value="$(
    kubectl --kubeconfig "$kubeconfig" \
      -n "$namespace" \
      get "$resource" "$name" \
      -o "jsonpath={range .status.conditions[?(@.type=='$condition')]}{.status}{' '}{.reason}{' - '}{.message}{end}" 2>/dev/null || true
  )"
  if [ -z "$value" ]; then
    printf '  %-32s %s\n' "$label:" "not observed"
    return
  fi

  case "$value" in
    True\ *)
      status_reason="${value%% - *}"
      suffix="${value#"$status_reason"}"
      printf '  %-32s %s%s\n' "$label:" "$(task_green "$status_reason")" "$suffix"
      ;;
    *)
      printf '  %-32s %s\n' "$label:" "$value"
      ;;
  esac
}

task_replica_summary() {
  local kubeconfig="$1"
  local namespace="$2"
  local tamoss_name="$3"
  local component="$4"
  local value
  local available desired display

  value="$(
    kubectl --kubeconfig "$kubeconfig" \
      -n "$namespace" \
      get tamoss "$tamoss_name" \
      -o "jsonpath={.status.replicas.${component}.available}{' / '}{.status.replicas.${component}.desired}" 2>/dev/null || true
  )"
  if [ "$value" = " / " ] || [ -z "$value" ]; then
    value="not enabled"
  else
    available="${value%% / *}"
    desired="${value##* / }"
    display="$value available"
    if [[ "$available" =~ ^[0-9]+$ ]] &&
      [[ "$desired" =~ ^[0-9]+$ ]] &&
      [ "$desired" -gt 0 ] &&
      [ "$available" -eq "$desired" ]; then
      value="$(task_green "$display")"
    else
      value="$display"
    fi
  fi
  printf '  %-32s %s\n' "${component} replicas:" "$value"
}

task_summary_base_domain_for_profile() {
  local profile="$1"
  local base_domain="$2"

  if [ -n "$base_domain" ]; then
    printf '%s\n' "$base_domain"
    return
  fi
  case "$profile" in
    local-kind) printf 'tamoss.localtest.me\n' ;;
  esac
}

task_summary_component_url() {
  local component="$1"
  local base_domain="$2"
  local scheme="${3:-https}"

  if [ -z "$base_domain" ]; then
    return 0
  fi
  base_domain="${base_domain#http://}"
  base_domain="${base_domain#https://}"
  base_domain="${base_domain%%/*}"
  if [ -z "$base_domain" ]; then
    return 0
  fi
  printf '%s://%s.%s\n' "$scheme" "$component" "$base_domain"
}

task_summary_auth_namespace() {
  local kubeconfig="$1"
  local namespace="$2"
  local tamoss_name="$3"
  local fallback="$4"
  local value

  value="$(
    task_k8s_resource_value \
      "$kubeconfig" \
      "$namespace" \
      "tamoss/$tamoss_name" \
      "{.spec.auth.authentikBlueprints.platformNamespace}"
  )"
  printf '%s\n' "${value:-$fallback}"
}

task_summary_oauth_issuer() {
  local auth_url="$1"
  local application_slug="$2"

  if [ -z "$auth_url" ] || [ -z "$application_slug" ]; then
    return 0
  fi
  printf '%s/application/o/%s/\n' "${auth_url%/}" "$application_slug"
}

task_print_env_summary() {
  local environment_dir="$1"
  local kubeconfig="$2"
  local target_file="${3:-}"
  local rendered profile api_namespace tamoss_name base_domain
  local app_url api_url auth_url token_key
  local token_resource_name token_secret bearer_token default_storagebackend ready_status phase
  local s3_url app_username app_username_secret app_username_key app_username_namespace
  local app_password app_password_secret app_password_key app_password_namespace
  local generated_api_token_secret generated_oauth_secret oauth_secret oauth_secret_namespace
  local oauth_client_id oauth_client_secret oauth_issuer oauth_token_endpoint application_slug auth_provider
  local auth_namespace live_api_url live_ui_url live_auth_url live_s3_url external_oauth_secret

  rendered="$(mktemp)"
  task_render_environment \
    "$environment_dir" \
    "$rendered" \
    "Environment $environment_dir was not found. Create it with task env:init."
  profile="$(task_tamoss_field_from_rendered "$rendered" profile)"
  api_namespace="$(task_tamoss_field_from_rendered "$rendered" namespace)"
  tamoss_name="$(task_tamoss_field_from_rendered "$rendered" name)"
  base_domain="$(task_tamoss_field_from_rendered "$rendered" baseDomain)"
  rm -f "$rendered"

  api_namespace="${api_namespace:-tams}"
  base_domain="$(task_summary_base_domain_for_profile "$profile" "$base_domain")"
  if [ -z "$tamoss_name" ]; then
    echo "Unable to infer Tamoss metadata.name from $environment_dir." >&2
    return 1
  fi

  if [ -n "$target_file" ] && [ -f "$target_file" ]; then
    set -a
    # shellcheck disable=SC1090
    . "$target_file"
    set +a
  fi

  live_api_url="$(
    task_k8s_resource_value "$kubeconfig" "$api_namespace" "tamoss/$tamoss_name" "{.status.endpoints.api}"
  )"
  live_ui_url="$(
    task_k8s_resource_value "$kubeconfig" "$api_namespace" "tamoss/$tamoss_name" "{.status.endpoints.ui}"
  )"
  live_auth_url="$(
    task_k8s_resource_value "$kubeconfig" "$api_namespace" "tamoss/$tamoss_name" "{.spec.auth.authentikBlueprints.issuerURL}"
  )"
  live_s3_url="$(
    task_k8s_resource_value "$kubeconfig" "$api_namespace" "tamoss/$tamoss_name" "{.spec.backends.s3.rustfsOperator.publicEndpoint.url}"
  )"
  if [ -z "$live_s3_url" ]; then
    live_s3_url="$(
      task_k8s_resource_value "$kubeconfig" "$api_namespace" "tamoss/$tamoss_name" "{.spec.backends.s3.external.endpoint.public.url}"
    )"
  fi

  app_url="${TEST_TAMOSS_UI:-${live_ui_url:-$(task_summary_component_url app "$base_domain")}}"
  api_url="${TEST_TAMOSS_API:-${live_api_url:-$(task_summary_component_url api "$base_domain")}}"
  auth_url="${TEST_TAMOSS_AUTH:-${live_auth_url:-$(task_summary_component_url auth "$base_domain")}}"
  s3_url="${TEST_TAMOSS_S3:-${live_s3_url:-$(task_summary_component_url s3 "$base_domain")}}"
  token_key="${TEST_TAMOSS_TOKEN_KEY:-TAMOSS_API_TOKEN}"
  token_resource_name="$(
    kubectl --kubeconfig "$kubeconfig" \
      -n "$api_namespace" \
      get tamoss "$tamoss_name" \
      -o "jsonpath={.spec.fullnameOverride}" 2>/dev/null || true
  )"
  token_resource_name="${token_resource_name:-$tamoss_name}"
  generated_api_token_secret="$(
    task_k8s_resource_value "$kubeconfig" "$api_namespace" "tamoss/$tamoss_name" "{.status.resolved.generatedSecrets.apiToken}"
  )"
  token_secret="${TEST_TAMOSS_TOKEN_SECRET:-${generated_api_token_secret:-${token_resource_name}-api-token}}"

  bearer_token="$(task_k8s_secret_value "$kubeconfig" "$api_namespace" "$token_secret" "$token_key")"

  default_storagebackend="$(
    kubectl --kubeconfig "$kubeconfig" \
      -n "$api_namespace" \
      get tamoss "$tamoss_name" \
      -o "jsonpath={.status.resolved.resources.defaultStorageBackend}" 2>/dev/null || true
  )"

  auth_provider="$(
    task_k8s_resource_value "$kubeconfig" "$api_namespace" "tamoss/$tamoss_name" "{.status.auth.provider}"
  )"
  if [ -z "$auth_provider" ]; then
    auth_provider="$(
      task_k8s_resource_value "$kubeconfig" "$api_namespace" "tamoss/$tamoss_name" "{.spec.auth.providedBy}"
    )"
  fi
  app_username="${TEST_TAMOSS_AUTH_USER:-}"
  app_username_secret="${TEST_TAMOSS_AUTH_USER_SECRET:-}"
  app_username_key="${TEST_TAMOSS_AUTH_USER_KEY:-}"
  app_password="${TEST_TAMOSS_AUTH_PASSWORD:-}"
  app_password_secret="${TEST_TAMOSS_AUTH_PASSWORD_SECRET:-}"
  app_password_key="${TEST_TAMOSS_AUTH_PASSWORD_KEY:-}"
  auth_namespace="$(
    task_summary_auth_namespace \
      "$kubeconfig" \
      "$api_namespace" \
      "$tamoss_name" \
      "${TEST_TAMOSS_AUTH_NAMESPACE:-auth}"
  )"
  app_password_secret="${app_password_secret:-authentik}"
  app_password_key="${app_password_key:-AUTHENTIK_BOOTSTRAP_PASSWORD}"
  app_password_namespace="${TEST_TAMOSS_AUTH_NAMESPACE:-$auth_namespace}"
  app_username_secret="${app_username_secret:-$app_password_secret}"
  app_username_key="${app_username_key:-AUTHENTIK_BOOTSTRAP_USERNAME}"
  app_username_namespace="${TEST_TAMOSS_AUTH_USER_NAMESPACE:-${TEST_TAMOSS_AUTH_NAMESPACE:-$auth_namespace}}"
  if [ -z "$app_username" ] && [ -n "$app_username_secret" ] && [ -n "$app_username_key" ]; then
    app_username="$(
      task_k8s_secret_value \
        "$kubeconfig" \
        "$app_username_namespace" \
        "$app_username_secret" \
        "$app_username_key"
    )"
  fi
  if [ -z "$app_username" ] && [ "$auth_provider" = "authentik-blueprints" ]; then
    app_username="akadmin"
  fi
  if [ -z "$app_password" ] && [ -n "$app_password_secret" ] && [ -n "$app_password_key" ]; then
    app_password="$(
      kubectl --kubeconfig "$kubeconfig" \
        -n "$app_password_namespace" \
        get secret "$app_password_secret" \
        -o "jsonpath={.data.${app_password_key}}" 2>/dev/null \
      | base64 --decode || true
    )"
  fi

  application_slug="$(
    task_k8s_resource_value "$kubeconfig" "$api_namespace" "tamoss/$tamoss_name" "{.status.auth.applicationSlug}"
  )"
  generated_oauth_secret="$(
    task_k8s_resource_value "$kubeconfig" "$api_namespace" "tamoss/$tamoss_name" "{.status.resolved.generatedSecrets.oauth2Credentials}"
  )"
  external_oauth_secret="$(
    task_k8s_resource_value "$kubeconfig" "$api_namespace" "tamoss/$tamoss_name" "{.spec.auth.external.oauth2.clientCredentialsSecret.existingSecret}"
  )"
  oauth_secret_namespace="${TEST_TAMOSS_OAUTH_CLIENT_SECRET_NAMESPACE:-$api_namespace}"
  oauth_secret="${TEST_TAMOSS_OAUTH_CLIENT_SECRET_NAME:-${generated_oauth_secret:-$external_oauth_secret}}"
  oauth_client_id="${TEST_TAMOSS_OAUTH_CLIENT_ID:-}"
  if [ -z "$oauth_client_id" ] && [ -n "$oauth_secret" ]; then
    oauth_client_id="$(
      task_k8s_secret_first_value \
        "$kubeconfig" \
        "$oauth_secret_namespace" \
        "$oauth_secret" \
        "${TEST_TAMOSS_OAUTH_CLIENT_ID_SECRET_KEY:-TAMOSS_OAUTH_CLIENT_ID}" \
        client_id
    )"
  fi
  oauth_client_secret="${TEST_TAMOSS_OAUTH_CLIENT_SECRET:-}"
  if [ -z "$oauth_client_secret" ] && [ -n "$oauth_secret" ]; then
    oauth_client_secret="$(
      task_k8s_secret_first_value \
        "$kubeconfig" \
        "$oauth_secret_namespace" \
        "$oauth_secret" \
        "${TEST_TAMOSS_OAUTH_CLIENT_SECRET_KEY:-TAMOSS_OAUTH_CLIENT_SECRET}" \
        client_secret
    )"
  fi
  oauth_issuer="${TEST_TAMOSS_OAUTH_ISSUER:-}"
  if [ -z "$oauth_issuer" ] && [ "$auth_provider" = "authentik-blueprints" ]; then
    oauth_issuer="$(task_summary_oauth_issuer "$auth_url" "$application_slug")"
  fi
  if [ -z "$oauth_issuer" ]; then
    oauth_issuer="$(
      task_k8s_resource_value "$kubeconfig" "$api_namespace" "tamoss/$tamoss_name" "{.spec.auth.external.oauth2.issuer}"
    )"
  fi
  if [ -n "$oauth_issuer" ]; then
    oauth_issuer="${oauth_issuer%/}/"
  fi
  oauth_token_endpoint="${TEST_TAMOSS_OAUTH_TOKEN_ENDPOINT:-}"
  if [ -z "$oauth_token_endpoint" ] &&
    [ "$auth_provider" = "authentik-blueprints" ] &&
    [ -n "$auth_url" ]; then
    oauth_token_endpoint="${auth_url%/}/application/o/token/"
  fi

  rustfs_secret="${TEST_TAMOSS_RUSTFS_SECRET:-}"
  if [ -z "$rustfs_secret" ]; then
    rustfs_secret="$(
      kubectl --kubeconfig "$kubeconfig" \
        -n "$api_namespace" \
        get tamoss "$tamoss_name" \
        -o "jsonpath={.spec.backends.s3.rustfsOperator.credsSecret.existingSecret}" 2>/dev/null || true
    )"
  fi
  rustfs_secret="${rustfs_secret:-${token_resource_name}-s3-creds}"
  rustfs_username="${TEST_TAMOSS_RUSTFS_USERNAME:-${TEST_TAMOSS_RUSTFS_ACCESS_KEY:-}}"
  rustfs_password="${TEST_TAMOSS_RUSTFS_PASSWORD:-${TEST_TAMOSS_RUSTFS_SECRET_KEY:-}}"
  if [ -z "$rustfs_username" ]; then
    rustfs_username="$(
      task_k8s_secret_value \
        "$kubeconfig" \
        "$api_namespace" \
        "$rustfs_secret" \
        "${TEST_TAMOSS_RUSTFS_USERNAME_KEY:-RUSTFS_ACCESS_KEY}"
    )"
  fi
  if [ -z "$rustfs_username" ]; then
    rustfs_username="$(
      task_k8s_secret_value "$kubeconfig" "$api_namespace" "$rustfs_secret" accesskey
    )"
  fi
  if [ -z "$rustfs_password" ]; then
    rustfs_password="$(
      task_k8s_secret_value \
        "$kubeconfig" \
        "$api_namespace" \
        "$rustfs_secret" \
        "${TEST_TAMOSS_RUSTFS_PASSWORD_KEY:-RUSTFS_SECRET_KEY}"
    )"
  fi
  if [ -z "$rustfs_password" ]; then
    rustfs_password="$(
      task_k8s_secret_value "$kubeconfig" "$api_namespace" "$rustfs_secret" secretkey
    )"
  fi

  phase="$(
    task_k8s_resource_value "$kubeconfig" "$api_namespace" "tamoss/$tamoss_name" "{.status.phase}"
  )"
  ready_status="$(
    task_k8s_resource_value "$kubeconfig" "$api_namespace" "tamoss/$tamoss_name" "{range .status.conditions[?(@.type=='Ready')]}{.status}{end}"
  )"
  if [ "$ready_status" = "True" ]; then
    printf '\nTAMOSS is %s\n' "$(task_green "ready")"
  else
    printf '\nTAMOSS is %s\n' "${phase:-unknown}"
  fi
  printf '\nFirst-start lifecycle\n'
  task_condition_summary "$kubeconfig" "$api_namespace" "Dependencies and backends" tamoss "$tamoss_name" "BackendsReady"
  if [ -n "$default_storagebackend" ]; then
    task_condition_summary "$kubeconfig" "$api_namespace" "Default bucket" storagebackend "$default_storagebackend" "BucketReady"
    task_condition_summary "$kubeconfig" "$api_namespace" "Storage DB registration" storagebackend "$default_storagebackend" "DatabaseReady"
  else
    printf '  %-32s %s\n' "Default bucket:" "not managed"
    printf '  %-32s %s\n' "Storage DB registration:" "not managed"
  fi
  task_condition_summary "$kubeconfig" "$api_namespace" "Schema migration" tamoss "$tamoss_name" "SchemaMigrated"
  task_condition_summary "$kubeconfig" "$api_namespace" "Identity" tamoss "$tamoss_name" "IdentityReady"
  task_replica_summary "$kubeconfig" "$api_namespace" "$tamoss_name" api
  task_replica_summary "$kubeconfig" "$api_namespace" "$tamoss_name" ui
  task_replica_summary "$kubeconfig" "$api_namespace" "$tamoss_name" worker
  task_condition_summary "$kubeconfig" "$api_namespace" "Routes" tamoss "$tamoss_name" "RoutingReady"
  printf '\nAccess\n'
  printf '  App URL:          %s\n' "$app_url"
  printf '  Auth Admin URL:   %s/if/admin/\n' "${auth_url%/}"
  if [ -n "$app_username" ] || [ -n "$app_password" ]; then
    printf '  App/Auth User:    %s\n' "${app_username:-<not configured>}"
    printf '  App/Auth Pass:    %s\n' "${app_password:-<not available>}"
  fi
  printf '\n'
  printf '  API URL:          %s\n' "${api_url%/}"
  printf '  API Token:        %s\n' "${bearer_token:-<not available>}"
  printf '\n'
  printf 'OAuth2\n'
  printf '  Client ID:        %s\n' "${oauth_client_id:-<not available>}"
  printf '  Client Secret:    %s\n' "${oauth_client_secret:-<not available>}"
  printf '  Issuer URL:       %s\n' "${oauth_issuer:-<not available>}"
  printf '  Token URL:        %s\n' "${oauth_token_endpoint:-<not available>}"
  printf '\n'
  printf '  RustFS Admin URL: %s/rustfs/console/\n' "${s3_url%/}"
  printf '  RustFS Username:  %s\n' "${rustfs_username:-<not available>}"
  printf '  RustFS Password:  %s\n\n' "${rustfs_password:-<not available>}"
}
