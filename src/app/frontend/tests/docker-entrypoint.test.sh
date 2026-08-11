#!/bin/sh
set -eu

frontend_root="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT

run_entrypoint() {
  mkdir -p "$tmp_dir/html"
  printf '<html></html>\n' > "$tmp_dir/html/index.html.template"
  TAMOSS_UI_HTML_DIR="$tmp_dir/html" \
  TAMOSS_UI_INDEX_TEMPLATE="$tmp_dir/html/index.html.template" \
  TAMOSS_UI_INDEX_PATH="$tmp_dir/html/index.html" \
  TAMOSS_UI_RUNTIME_CONFIG_PATH="$tmp_dir/runtime-config.js" \
  TAMOSS_UI_RUNTIME_CONF_PATH="$tmp_dir/runtime.conf" \
  TAMOSS_API_UPSTREAM="http://tams-api:8000" \
  TAMOSS_CONSOLE_UPSTREAM="${1:-}" \
    sh "$frontend_root/docker-entrypoint.sh" true
}

run_entrypoint ""
grep -q '"apiUrl":"/api"' "$tmp_dir/runtime-config.js"
grep -q 'limit_except GET HEAD OPTIONS' "$tmp_dir/runtime.conf"
if grep -q 'controlApiUrl\|location /ui-api/' "$tmp_dir/runtime-config.js" "$tmp_dir/runtime.conf"; then
  echo "console configuration was emitted without an upstream" >&2
  exit 1
fi

run_entrypoint "http://tamoss-console:8081"
grep -q '"controlApiUrl":"/ui-api/v1"' "$tmp_dir/runtime-config.js"
grep -q 'location /ui-api/' "$tmp_dir/runtime.conf"
grep -q 'proxy_pass http://tamoss-console:8081;' "$tmp_dir/runtime.conf"
grep -q 'proxy_buffering off;' "$tmp_dir/runtime.conf"
grep -q 'proxy_read_timeout 1h;' "$tmp_dir/runtime.conf"

if run_entrypoint 'file:///tmp/socket' >/dev/null 2>&1; then
  echo "invalid console upstream was accepted" >&2
  exit 1
fi
