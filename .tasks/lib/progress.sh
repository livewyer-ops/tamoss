# Shared progress output for Taskfile commands.

task_debug_enabled() {
  case "${TASK_DEBUG:-${DEBUG:-}}" in
    1|true|TRUE|yes|YES|on|ON) return 0 ;;
    *) return 1 ;;
  esac
}

task_log_dir() {
  printf '%s\n' "${TASK_LOG_DIR:-.local/logs/task}"
}

task_log_slug() {
  printf '%s' "$1" \
    | tr '[:upper:]' '[:lower:]' \
    | sed -E 's/[^a-z0-9]+/-/g; s/^-+//; s/-+$//'
}

task_format_duration() {
  seconds="$1"
  if [ "$seconds" -lt 60 ]; then
    printf '%ss' "$seconds"
    return
  fi

  minutes=$((seconds / 60))
  seconds=$((seconds % 60))
  printf '%sm%02ss' "$minutes" "$seconds"
}

task_run_step_command() {
  TASK_STEP_STATUS=0

  set +e
  "$@"
  TASK_STEP_STATUS=$?
  set -e

  return 0
}

task_step() {
  label="$1"
  shift

  ok_symbol="${TASK_STEP_OK:-✓}"
  fail_symbol="${TASK_STEP_FAIL:-✗}"
  run_symbol="${TASK_STEP_RUN:-•}"

  if task_debug_enabled; then
    start_time="$(date +%s)"
    printf '%s %s\n' "$run_symbol" "$label"
    task_run_step_command "$@"
    step_status="$TASK_STEP_STATUS"
    duration="$(task_format_duration "$(($(date +%s) - start_time))")"
    if [ "$step_status" -eq 0 ]; then
      printf '\n%s %s (%s)\n' "$ok_symbol" "$label" "$duration"
    else
      printf '\n%s %s (%s)\n' "$fail_symbol" "$label" "$duration" >&2
    fi
    return "$step_status"
  fi

  log_dir="$(task_log_dir)"
  mkdir -p "$log_dir"
  log_file="$log_dir/$(date +%Y%m%d-%H%M%S)-$(task_log_slug "$label").log"

  start_time="$(date +%s)"
  printf '%s %s\n' "$run_symbol" "$label"
  task_run_step_command "$@" >"$log_file" 2>&1
  step_status="$TASK_STEP_STATUS"
  if [ "$step_status" -eq 0 ]; then
    duration="$(task_format_duration "$(($(date +%s) - start_time))")"
    printf '%s %s (%s)\n' "$ok_symbol" "$label" "$duration"
    return 0
  fi

  duration="$(task_format_duration "$(($(date +%s) - start_time))")"
  printf '%s %s (%s)\n' "$fail_symbol" "$label" "$duration" >&2
  printf 'Log: %s\n\n' "$log_file" >&2
  printf 'Last 80 lines:\n' >&2
  tail -80 "$log_file" >&2 || true
  return "$step_status"
}
