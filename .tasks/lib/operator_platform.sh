#!/usr/bin/env bash
set -euo pipefail

if [ -n "${BASH_SOURCE:-}" ]; then
  task_lib_dir="$(cd "$(dirname "${BASH_SOURCE}")" && pwd)"
else
  task_lib_dir="$(cd "${TASK_LIB_DIR:-.tasks/lib}" && pwd)"
fi
# shellcheck source=.tasks/lib/progress.sh
. "$task_lib_dir/progress.sh"

task_chart_dependency_version() {
  local chart_file="$1"
  local dependency="$2"

  yq -r ".dependencies[] | select((.alias // .name) == \"${dependency}\") | .version" "$chart_file"
}

task_check_platform_dependency_pins() {
  local dependencies_file="$1"
  local chart_file="deploy/platform/chart/Chart.yaml"
  local values_file="deploy/platform/chart/values.yaml"
  local cert_manager_version
  local cert_manager_chart_version
  local traefik_version
  local traefik_chart_version
  local authentik_version
  local authentik_chart_version
  local authentik_target_version
  local cnpg_version
  local cnpg_chart_version
  local rustfs_operator_tag
  local rustfs_version
  local expected_rustfs_image
  local compose_postgres_image
  local compose_rustfs_image

  cert_manager_version="$(yq -r ".spec.certManager.version" "$dependencies_file")"
  cert_manager_chart_version="$(yq -r ".spec.certManager.chartVersion" "$dependencies_file")"
  traefik_version="$(yq -r ".spec.traefik.version" "$dependencies_file")"
  traefik_chart_version="$(yq -r ".spec.traefik.chartVersion" "$dependencies_file")"
  authentik_version="$(yq -r ".spec.authentik.version" "$dependencies_file")"
  authentik_chart_version="$(yq -r ".spec.authentik.chartVersion" "$dependencies_file")"
  authentik_target_version="$(printf '%s\n' "$authentik_version" | sed -E 's/^([0-9]+[.][0-9]+).*/\1/')"
  cnpg_version="$(yq -r ".spec.cnpg.version" "$dependencies_file")"
  cnpg_chart_version="$(yq -r ".spec.cnpg.chartVersion" "$dependencies_file")"
  rustfs_operator_tag="$(yq -r ".spec.rustfsOperator.imageTag" "$dependencies_file")"
  rustfs_version="$(yq -r ".spec.rustfs.version" "$dependencies_file")"
  expected_rustfs_image="rustfs/rustfs:${rustfs_version}"
  compose_postgres_image="$(yq -r ".spec.compose.postgresImage" "$dependencies_file")"
  compose_rustfs_image="$(yq -r ".spec.compose.rustfsImage" "$dependencies_file")"

  test -f deploy/platform/chart/Chart.lock
  test -f "$(yq -r ".spec.traefik.crdManifest" "$dependencies_file")"
  test -f "$(yq -r ".spec.rustfsOperator.manifest" "$dependencies_file")"
  test -f "$(yq -r ".spec.rustfsOperator.crdManifest" "$dependencies_file")"

  test "$(task_chart_dependency_version "$chart_file" cert-manager)" = "$cert_manager_chart_version"
  test "$cert_manager_version" = "$cert_manager_chart_version"
  test "$(task_chart_dependency_version "$chart_file" traefik)" = "$traefik_chart_version"
  test "$(task_chart_dependency_version "$chart_file" authentikChart)" = "$authentik_chart_version"
  test "$authentik_version" = "$authentik_chart_version"
  test "$(task_chart_dependency_version "$chart_file" cnpg)" = "$cnpg_chart_version"

  test "$(yq -r ".traefik.image.tag" "$values_file")" = "$traefik_version"
  rg -q "Generated from traefik/traefik Helm chart ${traefik_chart_version} for Traefik ${traefik_version}" \
    deploy/platform/chart/files/traefik/traefik-crds.yaml
  rg -q "TargetAuthentikVersion[[:space:]]*=[[:space:]]*\"${authentik_target_version}\"" \
    operator/internal/controller/auth/authentik/blueprint.go
  rg -q "image: .*rustfs/operator:${rustfs_operator_tag}" deploy/platform/chart/files/rustfs-operator/rustfs-operator.yaml
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
