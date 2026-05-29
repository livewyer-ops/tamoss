#!/usr/bin/env sh
set -eu

script_dir="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=operator/test/chainsaw/scripts/lib.sh
. "$script_dir/lib.sh"

blueprint="${1:?blueprint name is required}"

chainsaw_authentik_debug blueprints "$blueprint"
