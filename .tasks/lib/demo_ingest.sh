#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=.tasks/lib/commands.sh
. "$script_dir/commands.sh"

target_file="${1:?target file is required}"
media_file="${2:?demo media file is required}"
kubeconfig="${3:?kubeconfig is required}"
audio_file="${4:?demo audio file is required}"

task_require_commands curl jq kubectl base64

if [ ! -f "$target_file" ]; then
  echo "Profile target file $target_file was not found." >&2
  exit 1
fi
if [ ! -s "$media_file" ]; then
  echo "Demo media fixture $media_file was not found or is empty." >&2
  exit 1
fi
if [ ! -s "$audio_file" ]; then
  echo "Demo audio fixture $audio_file was not found or is empty." >&2
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
audio_source_id="00000000-0000-4000-8000-000000000103"
audio_flow_id="00000000-0000-4000-8000-000000000104"
audio_object_id="tamoss-demo/tamoss-demo-audio.ts"
multi_source_id="00000000-0000-4000-8000-000000000105"
multi_flow_id="00000000-0000-4000-8000-000000000106"
timerange="[0:0_1:0)"
object_timerange="[1:600000000_2:600000000)"
ts_offset="-1:600000000"
last_duration="0:100000000"
key_frame_count="1"
audio_object_timerange="[1:400000000_2:424000000)"
audio_ts_offset="-1:400000000"
audio_last_duration="0:21333333"

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

ensure_demo_segment() {
  local segment_flow_id="$1"
  local segment_object_id="$2"
  local segment_media_file="$3"
  local segment_object_timerange="$4"
  local segment_ts_offset="$5"
  local segment_last_duration="$6"
  local segment_key_frame_count="${7:-}"
  local allocation_body
  local allocation_file="$tmp_dir/allocation-$segment_flow_id.json"
  local allocation_status
  local bytes
  local content_type
  local get_url
  local put_url
  local segment_body
  local segment_file="$tmp_dir/segment-$segment_flow_id.json"
  local segment_status
  local segments_file="$tmp_dir/segments-$segment_flow_id.json"
  local verify_file="$tmp_dir/verify-segments-$segment_flow_id.json"

  api_expect GET "/flows/$segment_flow_id/segments?limit=100&accept_storage_ids=$storage_id&presigned=true&verbose_storage=true" >"$segments_file"
  if ! jq -e --arg object_id "$segment_object_id" --arg timerange "$timerange" \
    'map(select(.object_id == $object_id and .timerange == $timerange)) | length > 0' \
    "$segments_file" >/dev/null; then
    allocation_body="$(
      jq -n \
        --arg object_id "$segment_object_id" \
        --arg storage_id "$storage_id" \
        '{object_ids: [$object_id], storage_id: $storage_id}'
    )"
    allocation_status="$(api_status POST "/flows/$segment_flow_id/storage" "$allocation_body" "$allocation_file")"
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
        --upload-file "$segment_media_file" \
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
        --arg object_id "$segment_object_id" \
        --arg timerange "$timerange" \
        --arg object_timerange "$segment_object_timerange" \
        --arg ts_offset "$segment_ts_offset" \
        --arg last_duration "$segment_last_duration" \
        --arg key_frame_count "$segment_key_frame_count" \
        '{
          object_id: $object_id,
          timerange: $timerange,
          object_timerange: $object_timerange,
          ts_offset: $ts_offset,
          last_duration: $last_duration
        } + if $key_frame_count == "" then {} else {
          key_frame_count: ($key_frame_count | tonumber)
        } end'
    )"
    segment_status="$(api_status POST "/flows/$segment_flow_id/segments" "$segment_body" "$segment_file")"
    if [ "$segment_status" -ne 201 ]; then
      if [ "$segment_status" -ne 400 ] || ! grep -qi "overlaps with an existing segment" "$segment_file"; then
        echo "Segment registration failed with HTTP $segment_status." >&2
        cat "$segment_file" >&2
        echo >&2
        exit 1
      fi
    fi
  fi

  api_expect GET "/flows/$segment_flow_id/segments?limit=1&object_id=$segment_object_id&accept_storage_ids=$storage_id&presigned=true&verbose_storage=true" >"$verify_file"
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
}

ensure_demo_segment \
  "$flow_id" "$object_id" "$media_file" "$object_timerange" \
  "$ts_offset" "$last_duration" "$key_frame_count"

audio_flow_payload="$(
  jq -n \
    --arg id "$audio_flow_id" \
    --arg source_id "$audio_source_id" \
    '{
      "id": $id,
      "source_id": $source_id,
      "format": "urn:x-nmos:format:audio",
      "codec": "audio/aac",
      "container": "video/mp2t",
      "label": "TAMOSS Demo Audio",
      "description": "Playable split audio ingested by task kind:up",
      "tags": {
        "tamoss-demo": "kind-up",
        "tamoss-ingest": "managed-fixture",
        "tamoss-ingest-timing": "fixture-probed"
      },
      "essence_parameters": {
        "sample_rate": 48000,
        "channels": 1
      }
    }'
)"
api_expect PUT "/flows/$audio_flow_id" "$audio_flow_payload" >/dev/null
ensure_demo_segment \
  "$audio_flow_id" "$audio_object_id" "$audio_file" "$audio_object_timerange" \
  "$audio_ts_offset" "$audio_last_duration"

multi_flow_payload="$(
  jq -n \
    --arg id "$multi_flow_id" \
    --arg source_id "$multi_source_id" \
    '{
      "id": $id,
      "source_id": $source_id,
      "format": "urn:x-nmos:format:multi",
      "label": "TAMOSS Demo Split",
      "description": "Split video and audio collection ingested by task kind:up",
      "tags": {
        "tamoss-demo": "kind-up",
        "tamoss-ingest": "managed-fixture"
      }
    }'
)"
api_expect PUT "/flows/$multi_flow_id" "$multi_flow_payload" >/dev/null

flow_collection_payload="$(
  jq -n \
    --arg video_flow_id "$flow_id" \
    --arg audio_flow_id "$audio_flow_id" \
    '[
      {id: $video_flow_id, role: "video"},
      {id: $audio_flow_id, role: "audio"}
    ]'
)"
api_expect PUT "/flows/$multi_flow_id/flow_collection" "$flow_collection_payload" >/dev/null
