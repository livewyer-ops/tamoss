#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=.tasks/lib/commands.sh
. "$script_dir/commands.sh"

target_file="${1:?target file is required}"
media_file="${2:?demo media file is required}"
kubeconfig="${3:?kubeconfig is required}"

task_require_commands curl jq kubectl base64

if [ ! -f "$target_file" ]; then
  echo "Profile target file $target_file was not found." >&2
  exit 1
fi
if [ ! -s "$media_file" ]; then
  echo "Demo media fixture $media_file was not found or is empty." >&2
  exit 1
fi

set -a
# shellcheck source=/dev/null
. "$target_file"
set +a

api_url="${TEST_TAMOSS_API:-https://api.tamoss.localtest.me}"
namespace="${TEST_TAMOSS_NAMESPACE:-tams}"
tamoss_name="${TEST_TAMOSS_CR_NAME:-tamoss-kind}"
token_key="${TEST_TAMOSS_TOKEN_KEY:-TAMOSS_API_TOKEN}"
token_secret="${TEST_TAMOSS_TOKEN_SECRET:-}"
token="${TEST_TAMOSS_TOKEN:-}"

if [ -z "$token_secret" ]; then
  token_resource_name="$(
    kubectl --kubeconfig "$kubeconfig" \
      -n "$namespace" \
      get tamoss "$tamoss_name" \
      -o "jsonpath={.spec.fullnameOverride}" 2>/dev/null || true
  )"
  token_resource_name="${token_resource_name:-$tamoss_name}"
  token_secret="${token_resource_name}-api-token"
fi

if [ -z "$token" ]; then
  token="$(
    kubectl --kubeconfig "$kubeconfig" \
      -n "$namespace" \
      get secret "$token_secret" \
      -o "jsonpath={.data.${token_key}}" \
    | base64 --decode
  )"
fi
if [ -z "$token" ]; then
  echo "Unable to load API token from $namespace/$token_secret key $token_key." >&2
  exit 1
fi

curl_tls_args=()
if [ "${TEST_INSECURE_SKIP_TLS_VERIFY:-false}" = "true" ]; then
  curl_tls_args=(-k)
fi

tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT

wait_for_api_ready() {
  local deadline
  local status
  local ready_file="$tmp_dir/readyz.json"
  local error_file="$tmp_dir/readyz.err"
  deadline="$(($(date +%s) + 180))"
  while [ "$(date +%s)" -lt "$deadline" ]; do
    status="$(
      curl -sS "${curl_tls_args[@]}" \
        -H "Accept: application/json" \
        -o "$ready_file" \
        -w "%{http_code}" \
        "${api_url%/}/readyz" \
        2>"$error_file" || true
    )"
    if [ "$status" = "200" ]; then
      return 0
    fi
    sleep 2
  done
  echo "Timed out waiting for API readiness at ${api_url%/}/readyz." >&2
  if [ -s "$ready_file" ]; then
    cat "$ready_file" >&2
    echo >&2
  fi
  if [ -s "$error_file" ]; then
    cat "$error_file" >&2
    echo >&2
  fi
  exit 1
}

api_status() {
  local method="$1"
  local path="$2"
  local body="$3"
  local output_file="$4"
  local url="${api_url%/}${path}"
  if [ -n "$body" ]; then
    curl -sS "${curl_tls_args[@]}" \
      -H "Authorization: Bearer $token" \
      -H "Accept: application/json" \
      -H "Content-Type: application/json" \
      -X "$method" \
      --data "$body" \
      -o "$output_file" \
      -w "%{http_code}" \
      "$url"
  else
    curl -sS "${curl_tls_args[@]}" \
      -H "Authorization: Bearer $token" \
      -H "Accept: application/json" \
      -X "$method" \
      -o "$output_file" \
      -w "%{http_code}" \
      "$url"
  fi
}

api_expect() {
  local method="$1"
  local path="$2"
  local body="${3:-}"
  local output_file="$tmp_dir/response.json"
  local status
  status="$(api_status "$method" "$path" "$body" "$output_file")"
  if [ "$status" -lt 200 ] || [ "$status" -ge 300 ]; then
    echo "API request failed: $method ${api_url%/}${path} returned HTTP $status." >&2
    cat "$output_file" >&2
    echo >&2
    exit 1
  fi
  cat "$output_file"
}

flow_id="00000000-0000-4000-8000-000000000102"
source_id="00000000-0000-4000-8000-000000000101"
object_id="tamoss-demo/tamoss-demo.ts"
timerange="[0:0_1:0)"
object_timerange="[1:600000000_2:600000000)"
ts_offset="-1:600000000"
last_duration="0:100000000"
key_frame_count="1"

wait_for_api_ready

flow_payload="$(
  jq -n \
    --arg id "$flow_id" \
    --arg source_id "$source_id" \
    --arg flow_label "TAMOSS Demo" \
    --arg description "Playable media ingested by task kind:up" \
    '{
      "id": $id,
      "source_id": $source_id,
      "format": "urn:x-nmos:format:video",
      "codec": "video/h264",
      "container": "video/mp2t",
      "label": $flow_label,
      "description": $description,
      "tags": {
        "tamoss-demo": "kind-up",
        "tamoss-ingest": "managed-fixture",
        "tamoss-ingest-timing": "fixture-probed"
      },
      "essence_parameters": {
        "frame_width": 64,
        "frame_height": 64,
        "frame_rate": {"numerator": 10, "denominator": 1}
      }
    }'
)"
api_expect PUT "/flows/$flow_id" "$flow_payload" >/dev/null

storage_backends="$(api_expect GET "/service/storage-backends")"
storage_id="$(
  printf '%s' "$storage_backends" | jq -r '
    (map(select(.default_storage == true))[0].id // .[0].id // empty)
  '
)"
if [ -z "$storage_id" ]; then
  echo "No storage backend is registered by ${api_url%/}/service/storage-backends." >&2
  exit 1
fi

segments_file="$tmp_dir/segments.json"
api_expect GET "/flows/$flow_id/segments?limit=100&accept_storage_ids=$storage_id&presigned=true&verbose_storage=true" >"$segments_file"
if jq -e --arg object_id "$object_id" --arg timerange "$timerange" \
  'map(select(.object_id == $object_id and .timerange == $timerange)) | length > 0' \
  "$segments_file" >/dev/null; then
  :
else
  allocation_body="$(
    jq -n \
      --arg object_id "$object_id" \
      --arg storage_id "$storage_id" \
      '{object_ids: [$object_id], storage_id: $storage_id}'
  )"
  allocation_file="$tmp_dir/allocation.json"
  allocation_status="$(api_status POST "/flows/$flow_id/storage" "$allocation_body" "$allocation_file")"
  if [ "$allocation_status" -eq 201 ]; then
    put_url="$(jq -r '.media_objects[0].put_url.url // empty' "$allocation_file")"
    content_type="$(jq -r '.media_objects[0].put_url.headers["Content-Type"] // "video/mp2t"' "$allocation_file")"
    if [ -z "$put_url" ]; then
      echo "Storage allocation did not return a presigned PUT URL." >&2
      cat "$allocation_file" >&2
      echo >&2
      exit 1
    fi
    curl -fsS "${curl_tls_args[@]}" \
      -X PUT \
      -H "Content-Type: $content_type" \
      --upload-file "$media_file" \
      "$put_url" >/dev/null
  elif [ "$allocation_status" -eq 400 ] && grep -qi "already exist" "$allocation_file"; then
    echo "Demo object already exists; verifying existing bytes." >&2
  else
    echo "Storage allocation failed with HTTP $allocation_status." >&2
    cat "$allocation_file" >&2
    echo >&2
    exit 1
  fi

  segment_body="$(
    jq -n \
      --arg object_id "$object_id" \
      --arg timerange "$timerange" \
      --arg object_timerange "$object_timerange" \
      --arg ts_offset "$ts_offset" \
      --arg last_duration "$last_duration" \
      --argjson key_frame_count "$key_frame_count" \
      '{
        object_id: $object_id,
        timerange: $timerange,
        object_timerange: $object_timerange,
        ts_offset: $ts_offset,
        last_duration: $last_duration,
        key_frame_count: $key_frame_count
      }'
  )"
  segment_file="$tmp_dir/segment.json"
  segment_status="$(api_status POST "/flows/$flow_id/segments" "$segment_body" "$segment_file")"
  if [ "$segment_status" -ne 201 ]; then
    if [ "$segment_status" -eq 400 ] && grep -qi "overlaps with an existing segment" "$segment_file"; then
      :
    else
      echo "Segment registration failed with HTTP $segment_status." >&2
      cat "$segment_file" >&2
      echo >&2
      exit 1
    fi
  fi
fi

verify_file="$tmp_dir/verify-segments.json"
api_expect GET "/flows/$flow_id/segments?limit=1&object_id=$object_id&accept_storage_ids=$storage_id&presigned=true&verbose_storage=true" >"$verify_file"
get_url="$(jq -r '.[0].get_urls[0].url // empty' "$verify_file")"
if [ -z "$get_url" ]; then
  echo "Demo segment did not return a presigned GET URL." >&2
  cat "$verify_file" >&2
  echo >&2
  exit 1
fi

bytes="$(
  curl -fsS "${curl_tls_args[@]}" \
    -L \
    -o /dev/null \
    -w "%{size_download}" \
    "$get_url"
)"
if [ "${bytes:-0}" -le 0 ]; then
  echo "Demo segment storage URL returned no bytes." >&2
  exit 1
fi
