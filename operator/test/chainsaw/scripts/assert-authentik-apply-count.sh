#!/usr/bin/env sh
set -eu

script_dir="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=operator/test/chainsaw/scripts/lib.sh
. "$script_dir/lib.sh"

blueprint="${1:?blueprint name is required}"
minimum="${2:-1}"
timeout="${3:-120}"

chainsaw_wait_authentik_apply_count "$blueprint" "$minimum" "$timeout"
