#!/usr/bin/env bash
set -euo pipefail

TASK_PROFILE_REGISTRY="${TASK_PROFILE_REGISTRY:-deploy/profiles.yaml}"

task_profiles_list() {
  yq -r '.profiles[].id' "$TASK_PROFILE_REGISTRY"
}

task_profiles_csv() {
  task_profiles_list | paste -sd, - | sed 's/,/, /g'
}

task_profile_query() {
  local profile="$1"
  local query="$2"

  PROFILE_VALUE="$profile" yq -r \
    ".profiles[] | select(.id == strenv(PROFILE_VALUE)) | ${query} // \"\"" \
    "$TASK_PROFILE_REGISTRY" \
    | sed -n '1p'
}

task_profile_field() {
  local profile="$1"
  local field="$2"
  local value

  value="$(task_profile_query "$profile" ".$field")"
  if [ -z "$value" ]; then
    echo "Unsupported PROFILE=$profile. Supported profiles: $(task_profiles_csv)" >&2
    return 2
  fi
  printf '%s\n' "$value"
}

task_profile_kind_config() {
  local profile="$1"

  task_profile_field "$profile" kindConfig
}

task_profile_remote_platform_dir() {
  local profile="$1"
  local value

  value="$(task_profile_query "$profile" ".remotePlatformKustomizeDir")"
  if [ -n "$value" ]; then
    printf '%s\n' "$value"
    return
  fi
  task_profile_field "$profile" platformKustomizeDir
}

task_profile_remote_enabled() {
  local profile="$1"

  [ "$(task_profile_query "$profile" ".remoteEnvironment")" = "true" ]
}

task_run_profile_e2e_sequence() {
  local project_name="$1"
  local kubeconfig="$2"
  local profile

  for profile in $(task_profiles_list); do
    task kind:e2e \
      PROFILE="$profile" \
      PROJECT_NAME="$project_name" \
      KUBECONFIG="$kubeconfig"
  done
}

task_validate_profile() {
  local profile="$1"
  local platform_dir
  local instance_dir
  local kind_config
  local target_env

  platform_dir="$(task_profile_field "$profile" platformKustomizeDir)"
  instance_dir="$(task_profile_field "$profile" instanceKustomizeDir)"
  kind_config="$(task_profile_field "$profile" kindConfig)"
  target_env="$(task_profile_field "$profile" targetEnv)"

  test -d "$platform_dir" || {
    echo "Profile platform path $platform_dir not found" >&2
    return 2
  }
  test -e "$instance_dir" || {
    echo "Profile instance path $instance_dir not found" >&2
    return 2
  }
  test -f "$kind_config" || {
    echo "Profile Kind config $kind_config not found" >&2
    return 2
  }
  test -f "$target_env" || {
    echo "Profile target file $target_env not found" >&2
    return 2
  }
}
