#!/bin/sh
# Generate public runtime configuration and the private nginx proxy boundary.

set -eu
umask 077

html_dir="${TAMOSS_UI_HTML_DIR:-/usr/share/nginx/html}"
index_template="${TAMOSS_UI_INDEX_TEMPLATE:-${html_dir}/index.html.template}"
index_path="${TAMOSS_UI_INDEX_PATH:-${html_dir}/index.html}"
runtime_config_path="${TAMOSS_UI_RUNTIME_CONFIG_PATH:-${html_dir}/runtime-config.js}"
runtime_conf_path="${TAMOSS_UI_RUNTIME_CONF_PATH:-/tmp/tams-ui-runtime.conf}"

validate_upstream_url() {
  url_name="$1"
  url_value="$2"
  if [ -z "$url_value" ]; then
    echo "${url_name} must be set" >&2
    exit 1
  fi
  case "$url_value" in
    http://*|https://*) ;;
    *)
      echo "${url_name} must start with http:// or https://" >&2
      exit 1
      ;;
  esac
  if ! printf '%s' "$url_value" | grep -Eq '^https?://[A-Za-z0-9._~-]+(:[0-9]{1,5})?(/[A-Za-z0-9._~:/?&=%+,-]*)?$'; then
    echo "${url_name} contains characters unsafe for nginx proxy_pass" >&2
    exit 1
  fi
}

nginx_escape() {
  printf '%s' "$1" | sed 's/\\/\\\\/g; s/"/\\"/g; s/\$/\\$/g'
}

read_forward_auth_proof() {
  proof_name="$1"
  proof_file="$2"
  if [ ! -f "$proof_file" ] || [ ! -r "$proof_file" ]; then
    echo "${proof_name} must name a readable proof file" >&2
    exit 1
  fi
  proof_value="$(cat "$proof_file")"
  if [ "${#proof_value}" -lt 32 ]; then
    echo "${proof_name} must contain at least 32 characters" >&2
    exit 1
  fi
  if [ "${#proof_value}" -gt 4096 ] || printf '%s' "$proof_value" | grep -q '[[:cntrl:]]'; then
    echo "${proof_name} is unsafe for an nginx request header" >&2
    exit 1
  fi
  nginx_escape "$proof_value"
}

auth_mode="${TAMOSS_UI_AUTH_MODE:-}"
case "$auth_mode" in
  authentik|none|unavailable) ;;
  *)
    echo "TAMOSS_UI_AUTH_MODE must be explicitly set to authentik, none, or unavailable" >&2
    exit 1
    ;;
esac

api_upstream="${TAMOSS_API_UPSTREAM:-}"
validate_upstream_url "TAMOSS_API_UPSTREAM" "$api_upstream"
api_upstream="${api_upstream%/}"

console_upstream="${TAMOSS_CONSOLE_UPSTREAM:-}"
if [ -n "$console_upstream" ]; then
  validate_upstream_url "TAMOSS_CONSOLE_UPSTREAM" "$console_upstream"
  console_upstream="${console_upstream%/}"
fi

cp "$index_template" "$index_path"
if [ -n "$console_upstream" ]; then
  cat > "$runtime_config_path" <<'EOF'
window.__TAMOSS_CONFIG__ = {"controlApiUrl":"/ui-api/v1"};
EOF
else
  cat > "$runtime_config_path" <<'EOF'
window.__TAMOSS_CONFIG__ = {};
EOF
fi

auth_location=""
auth_directives=""
api_trusted_identity_headers=""
console_trusted_identity_headers=""
if [ "$auth_mode" = "authentik" ]; then
  auth_request_url="${TAMOSS_AUTHENTIK_AUTH_REQUEST_URL:-}"
  validate_upstream_url "TAMOSS_AUTHENTIK_AUTH_REQUEST_URL" "$auth_request_url"

  api_proof_file="${TAMOSS_API_FORWARD_AUTH_SHARED_SECRET_FILE:-/run/tamoss/forward-auth/api-proof}"
  escaped_api_forward_auth_proof="$(read_forward_auth_proof TAMOSS_API_FORWARD_AUTH_SHARED_SECRET_FILE "$api_proof_file")"
  escaped_console_forward_auth_proof=""
  if [ -n "$console_upstream" ]; then
    console_proof_file="${TAMOSS_CONSOLE_FORWARD_AUTH_SHARED_SECRET_FILE:-/run/tamoss/forward-auth/console-proof}"
    escaped_console_forward_auth_proof="$(read_forward_auth_proof TAMOSS_CONSOLE_FORWARD_AUTH_SHARED_SECRET_FILE "$console_proof_file")"
  fi

  auth_location="
location = /_tamoss/auth {
    internal;
    proxy_pass ${auth_request_url};
    proxy_http_version 1.1;
    proxy_pass_request_body off;
    proxy_pass_request_headers off;
    proxy_set_header Content-Length \"\";
    proxy_set_header Authorization \"\";
    proxy_set_header Host \$http_host;
    proxy_set_header Cookie \$http_cookie;
    proxy_set_header X-Original-URL \"\$scheme://\$http_host\$request_uri\";
    proxy_set_header X-Original-Method \$request_method;
    proxy_set_header X-Forwarded-Host \$http_host;
    proxy_set_header X-Forwarded-Method \$request_method;
    proxy_set_header X-Forwarded-Proto \$tamoss_external_scheme;
    proxy_set_header X-Forwarded-Uri \$request_uri;
    proxy_set_header X-Real-IP \$remote_addr;
    proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
}"

  auth_directives="    auth_request /_tamoss/auth;
    auth_request_set \$tamoss_auth_subject \$upstream_http_x_authentik_uid;
    auth_request_set \$tamoss_auth_username \$upstream_http_x_authentik_username;
    auth_request_set \$tamoss_auth_groups \$upstream_http_x_authentik_groups;"

  trusted_identity_headers="    proxy_set_header X-TAMOSS-Forward-Auth-Subject \$tamoss_auth_subject;
    proxy_set_header X-TAMOSS-Forward-Auth-Username \$tamoss_auth_username;
    proxy_set_header X-TAMOSS-Forward-Auth-Groups \$tamoss_auth_groups;"
  api_trusted_identity_headers="${trusted_identity_headers}
    proxy_set_header X-TAMOSS-Forward-Auth-Secret \"${escaped_api_forward_auth_proof}\";"
  if [ -n "$console_upstream" ]; then
    console_trusted_identity_headers="${trusted_identity_headers}
    proxy_set_header X-TAMOSS-Forward-Auth-Secret \"${escaped_console_forward_auth_proof}\";"
  fi
fi

if [ "$auth_mode" = "unavailable" ]; then
  cat > "$runtime_conf_path" <<'EOF'
location = /readyz {
    access_log off;
    default_type text/plain;
    return 503 "browser authentication is unavailable";
}

location = /api {
    return 308 /api/;
}

location /api/ {
    default_type application/json;
    return 503 '{"code":"browser_auth_unavailable","error":"browser authentication is not configured"}';
}
EOF
  if [ -n "$console_upstream" ]; then
    cat >> "$runtime_conf_path" <<'EOF'

location = /ui-api {
    return 308 /ui-api/;
}

location /ui-api/ {
    default_type application/json;
    return 503 '{"code":"browser_auth_unavailable","error":"browser authentication is not configured"}';
}
EOF
  else
    cat >> "$runtime_conf_path" <<'EOF'

location = /ui-api {
    default_type application/json;
    return 503 '{"code":"console_unavailable","error":"Console API is not enabled for this TAMOSS instance."}';
}

location /ui-api/ {
    default_type application/json;
    return 503 '{"code":"console_unavailable","error":"Console API is not enabled for this TAMOSS instance."}';
}
EOF
  fi
  exec "$@"
fi

cat > "$runtime_conf_path" <<EOF
${auth_location}

location = /readyz {
    access_log off;
    default_type text/plain;
    return 200 "ready";
}

location = /api {
    return 308 /api/;
}

location /api/ {
    limit_except GET HEAD OPTIONS {
        deny all;
    }
${auth_directives}
    proxy_pass ${api_upstream}/;
    proxy_http_version 1.1;
    proxy_pass_request_body off;
    # Start from no browser headers. This strips Authorization, cookies,
    # Remote-User, and every X-TAMOSS-* and X-authentik-* spoofing attempt.
    proxy_pass_request_headers off;
    proxy_set_header Content-Length "";
    proxy_set_header Accept \$http_accept;
    proxy_set_header Cache-Control \$http_cache_control;
    proxy_set_header If-Match \$http_if_match;
    proxy_set_header If-Modified-Since \$http_if_modified_since;
    proxy_set_header If-None-Match \$http_if_none_match;
    proxy_set_header If-Range \$http_if_range;
    proxy_set_header Origin \$http_origin;
    proxy_set_header Range \$http_range;
    proxy_set_header User-Agent \$http_user_agent;
    proxy_set_header Host \$http_host;
    proxy_set_header X-Real-IP \$remote_addr;
    proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto \$tamoss_external_scheme;
    proxy_set_header X-Request-ID \$request_id;
${api_trusted_identity_headers}
}
EOF

if [ -n "$console_upstream" ]; then
  cat >> "$runtime_conf_path" <<EOF

location = /ui-api {
    return 308 /ui-api/;
}

location /ui-api/ {
${auth_directives}
    client_max_body_size 8k;
    proxy_pass ${console_upstream};
    proxy_http_version 1.1;
    # The Console API receives only protocol headers plus identity established
    # by the Authentik subrequest. Browser-supplied identity is never forwarded.
    proxy_pass_request_headers off;
    proxy_set_header Accept \$http_accept;
    proxy_set_header Cache-Control \$http_cache_control;
    proxy_set_header Content-Length \$content_length;
    proxy_set_header Content-Type \$content_type;
    proxy_set_header Last-Event-ID \$http_last_event_id;
    proxy_set_header Origin \$http_origin;
    proxy_set_header User-Agent \$http_user_agent;
    proxy_set_header Host \$http_host;
    proxy_set_header X-Real-IP \$remote_addr;
    proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto \$tamoss_external_scheme;
    proxy_set_header X-Request-ID \$request_id;
${console_trusted_identity_headers}
    proxy_set_header Connection "";
    proxy_buffering off;
    proxy_cache off;
    proxy_read_timeout 1h;
    proxy_send_timeout 1h;
    add_header X-Accel-Buffering "no" always;
    # This location defines its own add_header, so nginx stops inheriting the
    # server-level security headers. Repeat them here.
    add_header X-Frame-Options "SAMEORIGIN" always;
    add_header X-Content-Type-Options "nosniff" always;
    add_header Referrer-Policy "strict-origin-when-cross-origin" always;
    add_header Cross-Origin-Opener-Policy "same-origin" always;
    add_header Cross-Origin-Embedder-Policy "require-corp" always;
    add_header Cross-Origin-Resource-Policy "same-origin" always;
    add_header Content-Security-Policy \$tamoss_content_security_policy always;
}
EOF
else
  cat >> "$runtime_conf_path" <<'EOF'

location = /ui-api {
    default_type application/json;
    return 503 '{"code":"console_unavailable","error":"Console API is not enabled for this TAMOSS instance."}';
}

location /ui-api/ {
    default_type application/json;
    return 503 '{"code":"console_unavailable","error":"Console API is not enabled for this TAMOSS instance."}';
}
EOF
fi

exec "$@"
