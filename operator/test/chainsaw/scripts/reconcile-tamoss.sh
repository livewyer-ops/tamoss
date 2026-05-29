#!/usr/bin/env sh
set -eu

script_dir="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=operator/test/chainsaw/scripts/lib.sh
. "$script_dir/lib.sh"

chainsaw_require_namespace

if [ "$#" -eq 0 ]; then
  set -- tamoss
fi

for tamoss_name in "$@"; do
  chainsaw_reconcile_tamoss "$NAMESPACE" "$tamoss_name"
done
