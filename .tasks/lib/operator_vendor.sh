#!/usr/bin/env bash
set -euo pipefail

if [ -n "${BASH_SOURCE:-}" ]; then
  task_lib_dir="$(cd "$(dirname "${BASH_SOURCE}")" && pwd)"
else
  task_lib_dir="$(cd "${TASK_LIB_DIR:-.tasks/lib}" && pwd)"
fi
# shellcheck source=.tasks/lib/progress.sh
. "$task_lib_dir/progress.sh"

task_vendor_cert_manager_manifest() {
  local dependencies_file="$1"
  local version

  version="$(yq -r ".spec.certManager.version" "$dependencies_file")"
  curl -fsSL "https://github.com/cert-manager/cert-manager/releases/download/${version}/cert-manager.yaml" \
    > deploy/platform/components/cert-manager/cert-manager.yaml
}

task_vendor_cnpg_manifest() {
  local dependencies_file="$1"
  local version
  local version_no_v

  version="$(yq -r ".spec.cnpg.version" "$dependencies_file")"
  version_no_v="${version#v}"
  curl -fsSL "https://github.com/cloudnative-pg/cloudnative-pg/releases/download/${version}/cnpg-${version_no_v}.yaml" \
    > deploy/platform/components/cnpg/cnpg.yaml
}

task_vendor_authentik_manifest() {
  local dependencies_file="$1"
  local release="$2"
  local chart_repo="$3"
  local namespace="$4"
  local values_file="$5"
  local version

  version="$(yq -r ".spec.authentik.version" "$dependencies_file")"
  helm template "$release" authentik \
    --repo "$chart_repo" \
    --version "$version" \
    --namespace "$namespace" \
    --values "$values_file" \
    > deploy/platform/components/authentik/kind/authentik.yaml
}

task_vendor_traefik_crds() {
  local dependencies_file="$1"
  local chart_version
  local traefik_version

  chart_version="$(yq -r ".spec.traefik.chartVersion" "$dependencies_file")"
  traefik_version="$(yq -r ".spec.traefik.version" "$dependencies_file")"
  {
    printf "# Generated from traefik/traefik Helm chart %s for Traefik %s\n" "$chart_version" "$traefik_version"
    helm show crds traefik/traefik --version "$chart_version" | awk '
      BEGIN { RS="---\n"; ORS="" }
      $0 ~ /name: (ingressroutes|ingressroutetcps|ingressrouteudps|middlewares|middlewaretcps|serverstransports|serverstransporttcps|tlsoptions|tlsstores|traefikservices)\.traefik\.io/ {
        sub(/^---\n/, "", $0)
        print "---\n" $0
      }
    '
  } > deploy/platform/components/traefik/crds/traefik-crds.yaml
}

task_vendor_rustfs_operator_manifest() {
  local dependencies_file="$1"
  local rustfs_operator_dir="$2"
  local release="$3"
  local namespace="$4"
  local ref
  local image_tag
  local repo

  ref="$(yq -r ".spec.rustfsOperator.ref" "$dependencies_file")"
  image_tag="$(yq -r ".spec.rustfsOperator.imageTag" "$dependencies_file")"
  repo="$(yq -r ".spec.rustfsOperator.repository" "$dependencies_file")"
  if [ ! -d "$rustfs_operator_dir/.git" ]; then
    rm -rf "$rustfs_operator_dir"
    git clone --depth=1 "$repo" "$rustfs_operator_dir"
  fi
  git -C "$rustfs_operator_dir" fetch --depth=1 origin "$ref"
  git -C "$rustfs_operator_dir" checkout --detach FETCH_HEAD
  cp "$rustfs_operator_dir/deploy/rustfs-operator/crds/tenant.yaml" \
    deploy/platform/components/rustfs-operator/tenant-crd.yaml
  helm template "$release" "$rustfs_operator_dir/deploy/rustfs-operator" \
    --namespace "$namespace" \
    --set console.enabled=false \
    --set operator.image.tag="$image_tag" \
    > deploy/platform/components/rustfs-operator/rustfs-operator.yaml
}

task_vendor_platform_manifests() {
  local dependencies_file="$1"
  local authentik_release="$2"
  local authentik_chart_repo="$3"
  local authentik_namespace="$4"
  local authentik_values_file="$5"
  local rustfs_operator_dir="$6"
  local rustfs_operator_release="$7"
  local rustfs_operator_namespace="$8"

  task_step "vendor cert-manager manifest" \
    task_vendor_cert_manager_manifest "$dependencies_file"
  task_step "vendor CNPG manifest" \
    task_vendor_cnpg_manifest "$dependencies_file"
  task_step "vendor Authentik manifest" \
    task_vendor_authentik_manifest \
      "$dependencies_file" \
      "$authentik_release" \
      "$authentik_chart_repo" \
      "$authentik_namespace" \
      "$authentik_values_file"
  task_step "vendor Traefik CRDs" \
    task_vendor_traefik_crds "$dependencies_file"
  task_step "vendor RustFS Operator manifest" \
    task_vendor_rustfs_operator_manifest \
      "$dependencies_file" \
      "$rustfs_operator_dir" \
      "$rustfs_operator_release" \
      "$rustfs_operator_namespace"
}

task_check_platform_dependency_pins() {
  local dependencies_file="$1"
  local cert_manager_version
  local traefik_version
  local traefik_chart_version
  local authentik_version
  local authentik_target_version
  local cnpg_version
  local cnpg_image_tag
  local rustfs_operator_tag
  local rustfs_version
  local expected_rustfs_image
  local compose_postgres_image
  local compose_rustfs_image

  cert_manager_version="$(yq -r ".spec.certManager.version" "$dependencies_file")"
  traefik_version="$(yq -r ".spec.traefik.version" "$dependencies_file")"
  traefik_chart_version="$(yq -r ".spec.traefik.chartVersion" "$dependencies_file")"
  authentik_version="$(yq -r ".spec.authentik.version" "$dependencies_file")"
  authentik_target_version="$(printf '%s\n' "$authentik_version" | sed -E 's/^([0-9]+[.][0-9]+).*/\1/')"
  cnpg_version="$(yq -r ".spec.cnpg.version" "$dependencies_file")"
  cnpg_image_tag="${cnpg_version#v}"
  rustfs_operator_tag="$(yq -r ".spec.rustfsOperator.imageTag" "$dependencies_file")"
  rustfs_version="$(yq -r ".spec.rustfs.version" "$dependencies_file")"
  expected_rustfs_image="rustfs/rustfs:${rustfs_version}"
  compose_postgres_image="$(yq -r ".spec.compose.postgresImage" "$dependencies_file")"
  compose_rustfs_image="$(yq -r ".spec.compose.rustfsImage" "$dependencies_file")"

  test -f "$(yq -r ".spec.certManager.manifest" "$dependencies_file")"
  test -f "$(yq -r ".spec.cnpg.manifest" "$dependencies_file")"
  test -f "$(yq -r ".spec.authentik.manifest" "$dependencies_file")"
  test -f "$(yq -r ".spec.rustfsOperator.manifest" "$dependencies_file")"
  test -f "$(yq -r ".spec.rustfsOperator.crdManifest" "$dependencies_file")"
  rg -q "cert-manager-cainjector:${cert_manager_version}" deploy/platform/components/cert-manager/cert-manager.yaml
  rg -q "cert-manager-controller:${cert_manager_version}" deploy/platform/components/cert-manager/cert-manager.yaml
  rg -q "cert-manager-webhook:${cert_manager_version}" deploy/platform/components/cert-manager/cert-manager.yaml
  rg -q "image: traefik:${traefik_version}" deploy/platform/components/traefik
  rg -q "Generated from traefik/traefik Helm chart ${traefik_chart_version} for Traefik ${traefik_version}" \
    deploy/platform/components/traefik/crds/traefik-crds.yaml
  rg -q "app.kubernetes.io/version: \"?${authentik_version}\"?" deploy/platform/components/authentik/kind/authentik.yaml
  rg -q "TargetAuthentikVersion[[:space:]]*=[[:space:]]*\"${authentik_target_version}\"" \
    operator/internal/controller/auth/authentik/blueprint.go
  rg -q "image: ghcr.io/cloudnative-pg/cloudnative-pg:${cnpg_image_tag}" deploy/platform/components/cnpg/cnpg.yaml
  rg -q "image: .*rustfs/operator:${rustfs_operator_tag}" deploy/platform/components/rustfs-operator/rustfs-operator.yaml
  test -f deploy/platform/components/rustfs-operator/tenant-crd.yaml
  rg -q "DefaultRustFSImage[[:space:]]*=[[:space:]]*\"${expected_rustfs_image}\"" \
    operator/internal/controller/defaults/images.go
  rg -Fq "image: ${compose_postgres_image}" deploy/compose/docker-compose.yaml
  rg -Fq "image: ${compose_rustfs_image}" deploy/compose/docker-compose.yaml
  if [ "$compose_rustfs_image" != "$expected_rustfs_image" ]; then
    echo "compose RustFS image ${compose_rustfs_image} does not match platform RustFS ${expected_rustfs_image}" >&2
    return 1
  fi
  task_check_postgres_major_pin "$compose_postgres_image"
}

task_check_postgres_major_pin() {
  local canonical_image="$1"
  local canonical_major
  local failures=0

  canonical_major="$(printf '%s\n' "$canonical_image" | sed -E 's/.*:([0-9]+).*/\1/')"
  if [ -z "$canonical_major" ] || [ "$canonical_major" = "$canonical_image" ]; then
    echo "Unable to determine canonical Postgres major from ${canonical_image}" >&2
    return 1
  fi

  task_check_cnpg_postgres_default_source "$canonical_major" ||
    failures=1
  task_check_cnpg_postgres_default_yaml \
    operator/config/crd/bases/tamoss.livewyer.io_tamosses.yaml \
    "$canonical_major" ||
    failures=1
  task_check_cnpg_postgres_default_yaml \
    deploy/operator/install.yaml \
    "$canonical_major" ||
    failures=1

  while IFS= read -r match; do
    local path
    local line_number
    local text
    local observed_major

    path="${match%%:*}"
    match="${match#*:}"
    line_number="${match%%:*}"
    text="${match#*:}"
    observed_major=""

    observed_major="$(
      printf '%s\n' "$text" |
        sed -nE \
          -e 's/.*postgres:([0-9]+).*/\1/p' \
          -e 's/.*postgresVersion:[[:space:]]*"([0-9]+)".*/\1/p' \
          -e 's/.*DefaultCNPGPostgresVersion[[:space:]]*=[[:space:]]*"([0-9]+)".*/\1/p' |
        head -n 1
    )"

    if [ -n "$observed_major" ] && [ "$observed_major" != "$canonical_major" ]; then
      echo "${path}:${line_number}: Postgres major ${observed_major} does not match canonical ${canonical_major}" >&2
      failures=1
    fi
  done < <(
    rg -n \
      'postgres:[0-9]+|postgresVersion: "[0-9]+"|DefaultCNPGPostgresVersion[[:space:]]*=[[:space:]]*"[0-9]+"' \
      deploy/compose/docker-compose.yaml \
      deploy/platform/dependencies.yaml \
      docs \
      operator/api \
      operator/config \
      operator/internal/controller/defaults \
      operator/test/chainsaw
  )

  return "$failures"
}

task_check_cnpg_postgres_default_source() {
  local canonical_major="$1"

  awk -v major="$canonical_major" '
    /PostgresVersion string/ {
      if (previous !~ "kubebuilder:default=\"" major "\"") {
        print FILENAME ":" NR ": postgresVersion kubebuilder default does not match canonical " major > "/dev/stderr"
        exit 1
      }
    }
    { previous = $0 }
  ' operator/api/v1alpha1/tamoss_backend_types.go
}

task_check_cnpg_postgres_default_yaml() {
  local path="$1"
  local canonical_major="$2"

  awk -v major="\"${canonical_major}\"" '
    /postgresVersion:/ { in_postgres_version = 1 }
    in_postgres_version && /default:/ {
      if ($0 !~ "default: " major) {
        print FILENAME ":" NR ": postgresVersion default does not match canonical " major > "/dev/stderr"
        exit 1
      }
      found = 1
      in_postgres_version = 0
    }
    END {
      if (!found) {
        print FILENAME ": postgresVersion default was not found" > "/dev/stderr"
        exit 1
      }
    }
  ' "$path"
}
