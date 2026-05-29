#!/usr/bin/env bash
set -euo pipefail

task_require_commands() {
  for cmd in "$@"; do
    if ! command -v "$cmd" >/dev/null 2>&1; then
      echo "Required command '$cmd' was not found." >&2
      return 1
    fi
  done
}
