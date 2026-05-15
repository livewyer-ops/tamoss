#!/bin/sh
# Generate runtime browser configuration and nginx proxy settings.

set -eu

html_dir="${TAMOSS_UI_HTML_DIR:-/usr/share/nginx/html}"
index_template="${TAMOSS_UI_INDEX_TEMPLATE:-${html_dir}/index.html.template}"
index_path="${TAMOSS_UI_INDEX_PATH:-${html_dir}/index.html}"
runtime_config_path="${TAMOSS_UI_RUNTIME_CONFIG_PATH:-${html_dir}/runtime-config.js}"
runtime_conf_path="${TAMOSS_UI_RUNTIME_CONF_PATH:-/tmp/tams-ui-runtime.conf}"

json_escape() {
  printf '%s' "$1" |
    sed \
      -e 's/\\/\\\\/g' \
      -e 's/"/\\"/g' \
      -e ':a' \
      -e 'N' \
      -e '$!ba' \
      -e 's/\n/\\n/g'
}

api_url="${TAMOSS_API_URL:-/api}"
api_url_json="$(json_escape "$api_url")"

cp "$index_template" "$index_path"
cat > "$runtime_config_path" <<EOF
window.__TAMOSS_CONFIG__ = {"apiUrl":"${api_url_json}"};
EOF

api_upstream="${TAMOSS_API_UPSTREAM:-}"
if [ -z "$api_upstream" ]; then
  echo "TAMOSS_API_UPSTREAM must be set to the API proxy target URL" >&2
  exit 1
fi
case "$api_upstream" in
  http://*|https://*) ;;
  *)
    echo "TAMOSS_API_UPSTREAM must start with http:// or https://" >&2
    exit 1
    ;;
esac
if ! printf '%s' "$api_upstream" | grep -Eq '^https?://[A-Za-z0-9._~:/?#@!&%()+,=-]+$'; then
  echo "TAMOSS_API_UPSTREAM contains characters unsafe for nginx proxy_pass" >&2
  exit 1
fi
api_upstream="${api_upstream%/}"

auth_header=""
if [ -n "${TAMOSS_API_TOKEN:-}" ]; then
  if printf '%s' "$TAMOSS_API_TOKEN" | grep -q '[[:cntrl:]]'; then
    echo "TAMOSS_API_TOKEN contains control characters unsafe for nginx" >&2
    exit 1
  fi
  escaped_token="$(printf '%s' "$TAMOSS_API_TOKEN" | sed 's/\\/\\\\/g; s/"/\\"/g')"
  auth_header="    proxy_set_header Authorization \"Bearer ${escaped_token}\";"
fi

cat > "$runtime_conf_path" <<EOF
location = /api {
    return 308 /api/;
}

location /api/ {
    proxy_pass ${api_upstream}/;
    proxy_http_version 1.1;
    proxy_set_header Host \$host;
    proxy_set_header X-Real-IP \$remote_addr;
    proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto \$scheme;
${auth_header}
}
EOF

exec "$@"
