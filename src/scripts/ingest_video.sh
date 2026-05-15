#!/usr/bin/env bash
set -euo pipefail

# Ingest an MP4 video into TAMS by splitting it into segments and registering via API.
#
# Usage:
#   ./src/scripts/ingest_video.sh VIDEO_FILE [FLOW_LABEL] [FLOW_DESCRIPTION] [SEGMENT_DURATION]
#
# Arguments:
#   VIDEO_FILE         Path to MP4 video file (required)
#   FLOW_LABEL         Label for the flow (optional, defaults to filename)
#   FLOW_DESCRIPTION   Description for the flow (optional, defaults to auto-generated)
#   SEGMENT_DURATION   Segment duration in seconds (optional, defaults to 3)
#
# Environment variables:
#   FLOW_ID            Flow UUID (defaults to auto-generated)
#   SOURCE_ID          Source UUID (defaults to first video source, or a generated UUID)
#   BACKEND_ID         Storage backend UUID (defaults to first backend returned by API)
#   TAMOSS_API           API base URL (defaults to http://localhost:8000)
#   TAMOSS_UI_URL        UI base URL shown at completion (defaults to http://localhost:3000)
#   FRAMERATE          Video framerate for keyframe calculation (defaults to 25)
#   TAMOSS_TOKEN         Bearer token for API authentication (optional)
#   TAMOSS_BASIC_AUTH_USER / TAMOSS_BASIC_AUTH_PASSWORD
#                      Basic auth credentials for API authentication (optional)
#
# Example:
#   ./src/scripts/ingest_video.sh tests/fixtures/e2e/tiny-ingest.mp4
#   ./src/scripts/ingest_video.sh my-video.mp4 "My Video" "Description" 5
#   SEGMENT_DURATION=10 ./src/scripts/ingest_video.sh my-video.mp4

# Check for required tools
for cmd in ffmpeg curl jq uuidgen; do
  if ! command -v "$cmd" &> /dev/null; then
    echo "Error: Required command '$cmd' not found" >&2
    exit 1
  fi
done

# Parse arguments
if [ $# -lt 1 ]; then
  echo "Error: VIDEO_FILE is required" >&2
  echo "Usage: $0 VIDEO_FILE [FLOW_LABEL] [FLOW_DESCRIPTION] [SEGMENT_DURATION]" >&2
  exit 1
fi

VIDEO_FILE="$1"
if [ ! -f "$VIDEO_FILE" ]; then
  echo "Error: Video file '$VIDEO_FILE' not found" >&2
  exit 1
fi

# Extract filename without extension for default label
VIDEO_BASENAME=$(basename "$VIDEO_FILE" .mp4)

# Parse optional arguments with defaults
FLOW_LABEL="${2:-$VIDEO_BASENAME}"
SEGMENT_DURATION="${4:-${SEGMENT_DURATION:-3}}"
FLOW_DESCRIPTION="${3:-Video segmented into ${SEGMENT_DURATION}s chunks}"

# Environment variables with defaults
FLOW_ID="${FLOW_ID:-$(uuidgen | tr 'A-Z' 'a-z')}"
SOURCE_ID="${SOURCE_ID:-}"
API="${TAMOSS_API:-http://localhost:8000}"
UI_URL="${TAMOSS_UI_URL:-http://localhost:3000}"
FRAMERATE="${FRAMERATE:-25}"

CURL_ARGS=(-sS)
UPLOAD_CURL_ARGS=(-sS)
if [ "${TAMOSS_INSECURE_SKIP_TLS_VERIFY:-false}" = "true" ]; then
  CURL_ARGS+=(-k)
  UPLOAD_CURL_ARGS+=(-k)
fi
if [ -n "${TAMOSS_TOKEN:-}" ]; then
  CURL_ARGS+=(-H "Authorization: Bearer ${TAMOSS_TOKEN}")
elif [ -n "${TAMOSS_BASIC_AUTH_USER:-}" ] || [ -n "${TAMOSS_BASIC_AUTH_PASSWORD:-}" ]; then
  CURL_ARGS+=(-u "${TAMOSS_BASIC_AUTH_USER:-}:${TAMOSS_BASIC_AUTH_PASSWORD:-}")
fi

api_request() {
  local method="$1"
  local url="$2"
  local body="${3-}"
  local response
  local status
  local response_body
  local detail

  if [ "$#" -ge 3 ]; then
    response=$(curl "${CURL_ARGS[@]}" -X "$method" "$url" \
      -H "Content-Type: application/json" \
      -d "$body" \
      -w $'\n%{http_code}')
  else
    response=$(curl "${CURL_ARGS[@]}" -X "$method" "$url" -w $'\n%{http_code}')
  fi

  status=$(printf '%s\n' "$response" | tail -n1)
  response_body=$(printf '%s\n' "$response" | sed '$d')

  if [ "$status" -lt 200 ] || [ "$status" -ge 300 ]; then
    detail=$(printf '%s' "$response_body" | jq -r '.detail? // .message? // empty' 2>/dev/null || true)
    if [ -n "$detail" ]; then
      echo "Error: API request failed (${method} ${url}) [HTTP ${status}]: ${detail}" >&2
    else
      echo "Error: API request failed (${method} ${url}) [HTTP ${status}]" >&2
      printf '%s\n' "$response_body" >&2
    fi
    return 1
  fi

  printf '%s' "$response_body"
}

if [ -z "${BACKEND_ID:-}" ]; then
  BACKEND_ID=$(api_request GET "$API/service/storage-backends" | jq -r '.[0].id // empty')
  if [ -z "$BACKEND_ID" ]; then
    echo "Error: Unable to determine storage backend ID from $API/service/storage-backends" >&2
    exit 1
  fi
fi

if [ -z "${SOURCE_ID:-}" ]; then
  SOURCE_ID=$(api_request GET "$API/sources" | jq -r '
    (map(select(.format == "urn:x-nmos:format:video"))[0].id) //
    empty
  ')
  if [ -z "$SOURCE_ID" ]; then
    SOURCE_ID="$(uuidgen | tr 'A-Z' 'a-z')"
  fi
fi

# Create temporary directory for segments
TMP_DIR=$(mktemp -d)
trap 'rm -rf "$TMP_DIR"' EXIT

echo "Ingesting video: $VIDEO_FILE"
echo "  Flow ID:    $FLOW_ID"
echo "  Source ID:  $SOURCE_ID"
echo "  Label:      $FLOW_LABEL"
echo "  Segments:   ${SEGMENT_DURATION}s chunks"
echo ""

echo "Converting video into ${SEGMENT_DURATION}s TS chunks"
echo "  Output:     $TMP_DIR"

GOP_SIZE=$((FRAMERATE * SEGMENT_DURATION))

ffmpeg -y -loglevel error -i "$VIDEO_FILE" \
  -c:v libx264 -preset veryfast -g "$GOP_SIZE" -keyint_min "$GOP_SIZE" -sc_threshold 0 \
  -force_key_frames "expr:gte(t,n_forced*${SEGMENT_DURATION})" \
  -c:a aac -b:a 128k \
  -f segment -segment_time "$SEGMENT_DURATION" -reset_timestamps 1 -segment_format mpegts \
  "$TMP_DIR/chunk-%03d.ts"

CHUNK_COUNT=$(find "$TMP_DIR" -name "chunk-*.ts" | wc -l)
echo "  Created:    $CHUNK_COUNT segments"
echo ""

# Video flows must supply essence parameters on PUT.
if command -v ffprobe >/dev/null 2>&1; then
  FRAME_WIDTH=$(ffprobe -v error -select_streams v:0 -show_entries stream=width -of csv=p=0 "$VIDEO_FILE" 2>/dev/null || echo "1920")
  FRAME_HEIGHT=$(ffprobe -v error -select_streams v:0 -show_entries stream=height -of csv=p=0 "$VIDEO_FILE" 2>/dev/null || echo "1080")
  FR_RATIO=$(ffprobe -v error -select_streams v:0 -show_entries stream=r_frame_rate -of csv=p=0 "$VIDEO_FILE" 2>/dev/null || echo "25/1")
  FR_NUM="${FR_RATIO%%/*}"
  FR_DEN="${FR_RATIO##*/}"
else
  FRAME_WIDTH=1920
  FRAME_HEIGHT=1080
  FR_NUM="$FRAMERATE"
  FR_DEN=1
fi

echo "Creating flow $FLOW_ID"
FLOW_BODY=$(jq -n \
  --arg id "$FLOW_ID" \
  --arg flow_label "$FLOW_LABEL" \
  --arg description "$FLOW_DESCRIPTION" \
  --arg source_id "$SOURCE_ID" \
  --argjson frame_width "$FRAME_WIDTH" \
  --argjson frame_height "$FRAME_HEIGHT" \
  --argjson fr_num "$FR_NUM" \
  --argjson fr_den "$FR_DEN" \
  '{
    id: $id,
    "label": $flow_label,
    description: $description,
    format: "urn:x-nmos:format:video",
    source_id: $source_id,
    container: "video/mp2t",
    codec: "video/h264",
    essence_parameters: {
      frame_width: $frame_width,
      frame_height: $frame_height,
      frame_rate: {numerator: $fr_num, denominator: $fr_den}
    }
  }')
FLOW_RESPONSE=$(api_request PUT "$API/flows/$FLOW_ID" "$FLOW_BODY")
echo "$FLOW_RESPONSE" | jq -r '"  Status: \(.id // "created")"'

echo ""

echo "Allocating storage and uploading segments"

SEGMENTS_JSON="[]"
START_NS=0
SEGMENT_NUM=0

for TS_FILE in "$TMP_DIR"/chunk-*.ts; do
  SEGMENT_NUM=$((SEGMENT_NUM + 1))
  DURATION_NS=$((SEGMENT_DURATION * 1000000000))
  END_NS=$((START_NS + DURATION_NS))

  STORAGE_JSON=$(api_request POST "$API/flows/$FLOW_ID/storage" \
    "$(jq -n --arg storage_id "$BACKEND_ID" '{storage_id:$storage_id, limit:1}')")
  OBJECT_ID=$(echo "$STORAGE_JSON" | jq -r '.media_objects[0].object_id')
  PUT_URL=$(echo "$STORAGE_JSON" | jq -r '.media_objects[0].put_url.url')
  if [ -z "$OBJECT_ID" ] || [ "$OBJECT_ID" = "null" ] || [ -z "$PUT_URL" ] || [ "$PUT_URL" = "null" ]; then
    echo "Error: Storage allocation did not return an object_id and put_url" >&2
    printf '%s\n' "$STORAGE_JSON" >&2
    exit 1
  fi

  echo "  [$SEGMENT_NUM/$CHUNK_COUNT] Uploading $(basename "$TS_FILE") -> $OBJECT_ID"
  curl "${UPLOAD_CURL_ARGS[@]}" --fail-with-body -X PUT "$PUT_URL" -H "Content-Type: video/mp2t" --upload-file "$TS_FILE"

  START_SECONDS=$((START_NS / 1000000000))
  START_NANOS=$((START_NS % 1000000000))
  END_SECONDS=$((END_NS / 1000000000))
  END_NANOS=$((END_NS % 1000000000))
  TIMERANGE="[${START_SECONDS}:${START_NANOS}_${END_SECONDS}:${END_NANOS})"

  SEGMENTS_JSON=$(echo "$SEGMENTS_JSON" | jq \
    --arg obj "$OBJECT_ID" \
    --arg tr "$TIMERANGE" \
    '. + [{"object_id":$obj,"timerange":$tr}]')

  START_NS=$END_NS
done

echo ""

echo "Registering $CHUNK_COUNT segments with flow"
SEGMENT_RESPONSE=$(api_request POST "$API/flows/$FLOW_ID/segments" "$SEGMENTS_JSON")
if [ -z "$SEGMENT_RESPONSE" ]; then
  REGISTERED_COUNT="$CHUNK_COUNT"
else
  REGISTERED_COUNT=$(printf '%s' "$SEGMENT_RESPONSE" | jq -r --argjson chunk_count "$CHUNK_COUNT" '
    if type == "array" then length
    elif type == "object" and has("failed_segments") then 0
    elif type == "object" and has("id") then $chunk_count
    else 0
    end
  ')
fi
echo "  Registered: $REGISTERED_COUNT segment(s)"

echo ""
echo "Video ingestion complete"
echo "  Flow ID:    $FLOW_ID"
echo "  Label:      $FLOW_LABEL"
echo "  Segments:   $CHUNK_COUNT x ${SEGMENT_DURATION}s"
echo "  View at:    $UI_URL"
