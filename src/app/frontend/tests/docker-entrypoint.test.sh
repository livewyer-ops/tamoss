#!/bin/sh
set -eu

frontend_root="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT

run_entrypoint() {
  auth_mode="$1"
  console_upstream="${2:-}"
  auth_request_url="${3:-}"
  api_proof_file="${4:-}"
  console_proof_file="${5:-}"
  mkdir -p "$tmp_dir/html"
  printf '<html></html>\n' > "$tmp_dir/html/index.html.template"
  TAMOSS_UI_HTML_DIR="$tmp_dir/html" \
  TAMOSS_UI_INDEX_TEMPLATE="$tmp_dir/html/index.html.template" \
  TAMOSS_UI_INDEX_PATH="$tmp_dir/html/index.html" \
  TAMOSS_UI_RUNTIME_CONFIG_PATH="$tmp_dir/runtime-config.js" \
  TAMOSS_UI_RUNTIME_CONF_PATH="$tmp_dir/runtime.conf" \
  TAMOSS_UI_AUTH_MODE="$auth_mode" \
  TAMOSS_API_UPSTREAM="http://tams-api:8000" \
  TAMOSS_CONSOLE_UPSTREAM="$console_upstream" \
  TAMOSS_AUTHENTIK_AUTH_REQUEST_URL="$auth_request_url" \
  TAMOSS_API_FORWARD_AUTH_SHARED_SECRET_FILE="$api_proof_file" \
  TAMOSS_CONSOLE_FORWARD_AUTH_SHARED_SECRET_FILE="$console_proof_file" \
  TAMOSS_API_TOKEN="must-not-reach-the-browser-or-upstream" \
    sh "$frontend_root/docker-entrypoint.sh" true
}

run_entrypoint "none"
grep -q 'window.__TAMOSS_CONFIG__ = {}' "$tmp_dir/runtime-config.js"
grep -q 'return 200 "ready";' "$tmp_dir/runtime.conf"
grep -q 'limit_except GET HEAD OPTIONS' "$tmp_dir/runtime.conf"
grep -q 'proxy_pass_request_headers off;' "$tmp_dir/runtime.conf"
if grep -q 'must-not-reach\|Authorization "Bearer\|auth_request /_tamoss/auth' "$tmp_dir/runtime-config.js" "$tmp_dir/runtime.conf"; then
  echo "auth-none configuration exposed a credential or enabled auth_request" >&2
  exit 1
fi
if grep -q 'controlApiUrl\|proxy_pass .*console\|auth_request /_tamoss/auth' "$tmp_dir/runtime-config.js" "$tmp_dir/runtime.conf"; then
  echo "console proxy configuration was emitted without an upstream" >&2
  exit 1
fi
if [ "$(grep -c 'console_unavailable' "$tmp_dir/runtime.conf")" -ne 2 ]; then
  echo "disabled Console paths did not fail explicitly" >&2
  exit 1
fi
grep -q 'location = /ui-api {' "$tmp_dir/runtime.conf"
grep -q 'location /ui-api/ {' "$tmp_dir/runtime.conf"
grep -q 'default_type application/json;' "$tmp_dir/runtime.conf"

run_entrypoint "none" "http://tamoss-console:8081"
grep -q '"controlApiUrl":"/ui-api/v1"' "$tmp_dir/runtime-config.js"
grep -q 'location /ui-api/' "$tmp_dir/runtime.conf"
grep -q 'proxy_pass http://tamoss-console:8081;' "$tmp_dir/runtime.conf"
grep -q 'client_max_body_size 8k;' "$tmp_dir/runtime.conf"
test "$(grep -c 'proxy_set_header Host \$http_host;' "$tmp_dir/runtime.conf")" -eq 2
grep -q 'proxy_buffering off;' "$tmp_dir/runtime.conf"
grep -q 'proxy_read_timeout 1h;' "$tmp_dir/runtime.conf"

# Every proxied browser path reports one normalised external scheme, never the
# scheme a client asserted directly.
if [ "$(grep -c 'proxy_set_header X-Forwarded-Proto \$tamoss_external_scheme;' "$tmp_dir/runtime.conf")" -ne 2 ]; then
  echo "a proxied browser path did not use the normalised external scheme" >&2
  exit 1
fi
if grep -q 'proxy_set_header X-Forwarded-Proto \$scheme;' "$tmp_dir/runtime.conf"; then
  echo "a proxied browser path forwarded the unnormalised scheme" >&2
  exit 1
fi

# A location that defines add_header stops inheriting the server-level headers,
# so the streaming Console location must repeat the full set.
grep -q 'add_header X-Accel-Buffering "no" always;' "$tmp_dir/runtime.conf"
for header in \
  'add_header X-Frame-Options "SAMEORIGIN" always;' \
  'add_header X-Content-Type-Options "nosniff" always;' \
  'add_header Referrer-Policy "strict-origin-when-cross-origin" always;' \
  'add_header Cross-Origin-Opener-Policy "same-origin" always;' \
  'add_header Cross-Origin-Embedder-Policy "require-corp" always;' \
  'add_header Cross-Origin-Resource-Policy "same-origin" always;' \
  'add_header Content-Security-Policy $tamoss_content_security_policy always;'
do
  if ! grep -Fq "$header" "$tmp_dir/runtime.conf"; then
    echo "the Console location dropped the security header: $header" >&2
    exit 1
  fi
done

run_entrypoint "unavailable" "http://tamoss-console:8081"
grep -q 'return 503 "browser authentication is unavailable";' "$tmp_dir/runtime.conf"
if [ "$(grep -c 'browser_auth_unavailable' "$tmp_dir/runtime.conf")" -ne 2 ]; then
  echo "unavailable mode did not fail both browser backends closed" >&2
  exit 1
fi

run_entrypoint "unavailable"
if [ "$(grep -c 'browser_auth_unavailable' "$tmp_dir/runtime.conf")" -ne 1 ] || [ "$(grep -c 'console_unavailable' "$tmp_dir/runtime.conf")" -ne 2 ]; then
  echo "unavailable mode did not distinguish the API and disabled Console" >&2
  exit 1
fi
if grep -q 'proxy_pass\|auth_request /_tamoss/auth\|must-not-reach' "$tmp_dir/runtime.conf"; then
  echo "unavailable mode emitted a backend proxy or credential" >&2
  exit 1
fi

printf '%s' 'api-forward-proof-at-least-thirty-two-bytes$' > "$tmp_dir/api-forward-auth-proof"
printf '%s' 'console-forward-proof-at-least-thirty-two-bytes$' > "$tmp_dir/console-forward-auth-proof"
run_entrypoint \
  "authentik" \
  "http://tamoss-console:8081" \
  "http://authentik-outpost.auth.svc:9000/outpost.goauthentik.io/auth/nginx" \
  "$tmp_dir/api-forward-auth-proof" \
  "$tmp_dir/console-forward-auth-proof"
grep -q 'location = /_tamoss/auth' "$tmp_dir/runtime.conf"
test "$(grep -c 'proxy_set_header Host \$http_host;' "$tmp_dir/runtime.conf")" -eq 3
if [ "$(grep -c 'auth_request /_tamoss/auth;' "$tmp_dir/runtime.conf")" -ne 2 ]; then
  echo "both API proxy locations must require the Authentik subrequest" >&2
  exit 1
fi
grep -q 'proxy_pass_request_headers off;' "$tmp_dir/runtime.conf"
grep -q 'auth_request_set \$tamoss_auth_subject \$upstream_http_x_authentik_uid;' "$tmp_dir/runtime.conf"
grep -q 'proxy_set_header X-TAMOSS-Forward-Auth-Subject \$tamoss_auth_subject;' "$tmp_dir/runtime.conf"
grep -q 'proxy_set_header X-TAMOSS-Forward-Auth-Groups \$tamoss_auth_groups;' "$tmp_dir/runtime.conf"
if [ "$(grep -Fc 'proxy_set_header X-TAMOSS-Forward-Auth-Secret "api-forward-proof-at-least-thirty-two-bytes\$";' "$tmp_dir/runtime.conf")" -ne 1 ]; then
  echo "API proof was not isolated to one backend proxy" >&2
  exit 1
fi
if [ "$(grep -Fc 'proxy_set_header X-TAMOSS-Forward-Auth-Secret "console-forward-proof-at-least-thirty-two-bytes\$";' "$tmp_dir/runtime.conf")" -ne 1 ]; then
  echo "Console proof was not isolated to one backend proxy" >&2
  exit 1
fi
if grep -q 'proxy_set_header Remote-User\|proxy_set_header X-authentik-' "$tmp_dir/runtime.conf"; then
  echo "raw identity headers were forwarded to a backend" >&2
  exit 1
fi
if grep -q 'must-not-reach\|VITE_API_TOKEN' "$tmp_dir/runtime-config.js" "$tmp_dir/runtime.conf"; then
  echo "a browser or legacy API credential reached generated output" >&2
  exit 1
fi

run_entrypoint \
  "authentik" \
  "" \
  "http://authentik-outpost.auth.svc:9000/outpost.goauthentik.io/auth/nginx" \
  "$tmp_dir/api-forward-auth-proof"
if [ "$(grep -c 'auth_request /_tamoss/auth;' "$tmp_dir/runtime.conf")" -ne 1 ]; then
  echo "disabled Console configuration enabled an auth subrequest for the Console" >&2
  exit 1
fi
if [ "$(grep -c 'console_unavailable' "$tmp_dir/runtime.conf")" -ne 2 ] || grep -q 'console-forward-proof\|proxy_pass .*console' "$tmp_dir/runtime.conf"; then
  echo "disabled Console configuration emitted a Console proxy or proof" >&2
  exit 1
fi

if run_entrypoint "" >/dev/null 2>&1; then
  echo "an implicit auth mode was accepted" >&2
  exit 1
fi
if run_entrypoint "authentik" "" "" "$tmp_dir/api-forward-auth-proof" >/dev/null 2>&1; then
  echo "authentik mode accepted a missing auth_request URL" >&2
  exit 1
fi
if run_entrypoint "authentik" "" "http://authentik:9000/outpost.goauthentik.io/auth/nginx" "$tmp_dir/missing-proof" >/dev/null 2>&1; then
  echo "authentik mode accepted a missing proof file" >&2
  exit 1
fi
if run_entrypoint "authentik" "http://tamoss-console:8081" "http://authentik:9000/outpost.goauthentik.io/auth/nginx" "$tmp_dir/api-forward-auth-proof" "$tmp_dir/missing-proof" >/dev/null 2>&1; then
  echo "authentik mode accepted a missing Console proof file" >&2
  exit 1
fi
printf '%s' 'too-short' > "$tmp_dir/short-proof"
if run_entrypoint "authentik" "" "http://authentik:9000/outpost.goauthentik.io/auth/nginx" "$tmp_dir/short-proof" >/dev/null 2>&1; then
  echo "authentik mode accepted a short API proof" >&2
  exit 1
fi
if run_entrypoint "none" 'file:///tmp/socket' >/dev/null 2>&1; then
  echo "an invalid Console upstream was accepted" >&2
  exit 1
fi
if run_entrypoint "none" 'http://console:8081/;return' >/dev/null 2>&1; then
  echo "an nginx-directive injection was accepted in an upstream URL" >&2
  exit 1
fi

# The static server configuration the generated locations are included into.
nginx_conf="$frontend_root/nginx.conf"

# The external scheme is only taken from a trusted peer.
grep -q 'geo \$tamoss_forwarded_scheme_trusted {' "$nginx_conf"
grep -q 'map "\$tamoss_forwarded_scheme_trusted:\$http_x_forwarded_proto" \$tamoss_external_scheme {' "$nginx_conf"
if grep -q '^map \$http_x_forwarded_proto \$tamoss_external_scheme' "$nginx_conf"; then
  echo "the external scheme is mapped from the client header without a trusted edge" >&2
  exit 1
fi

# One Content-Security-Policy definition, repeated by every location that
# defines any add_header of its own.
grep -q 'map \$host \$tamoss_content_security_policy {' "$nginx_conf"
for directive in \
  "default-src 'self'" \
  "base-uri 'self'" \
  "object-src 'none'" \
  "frame-ancestors 'self'" \
  "script-src 'self' 'wasm-unsafe-eval' blob:" \
  "worker-src 'self' blob:" \
  "media-src 'self' data: blob: https:" \
  "connect-src 'self' data: blob: https:"
do
  if ! grep -Fq "$directive" "$nginx_conf"; then
    echo "the content security policy is missing: $directive" >&2
    exit 1
  fi
done
csp_headers="$(grep -c 'add_header Content-Security-Policy' "$nginx_conf")"
frame_headers="$(grep -c 'add_header X-Frame-Options' "$nginx_conf")"
if [ "$csp_headers" -ne "$frame_headers" ] || [ "$csp_headers" -lt 2 ]; then
  echo "a location sends the security header set without a content security policy" >&2
  exit 1
fi

# The health check must not cancel header inheritance with its own add_header.
if grep -q 'add_header Content-Type' "$nginx_conf"; then
  echo "the health check cancels security header inheritance" >&2
  exit 1
fi

# Request bodies are not proxied, so the container must not advertise a media
# ingestion body limit.
if grep -q 'client_max_body_size 500M;' "$nginx_conf"; then
  echo "the vestigial media ingestion body limit is still configured" >&2
  exit 1
fi
