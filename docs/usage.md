# Using TAMOSS

Direct API access, the web UI addon, and preview media workflows.

The TAMS API is the supported core surface. The web UI is an addon client.
UI-driven ingest and browser playback are preview workflows.

## Using the Web UI Addon

Access the web UI at <https://app.tamoss.localtest.me> after starting the stack
(see [deployment.md](./deployment.md)).

The UI is an addon client of the BBC TAMS API. Use it for operational browsing
and selected API-backed helper workflows; use the API itself as the authoritative
core contract.

### Flow Browser

- View available flows with metadata and segment summaries
- Open a flow to inspect essence metadata, storage links, and registered segments
- Use the flow detail page to allocate storage and register uploaded segments

### Operations

- Create flows and allocate storage through the BBC TAMS API.
- Monitor background deletion requests from the deletion queue page.
- Inspect service and storage backend state from the service page.

## Preview Ingesting Content

UI-driven ingest and `src/scripts/ingest_video.sh` are preview addon workflows.
They are useful for exercising the API and validating storage paths, but they
are not part of the core TAMS API conformance surface.

For the public product workflow, use the cluster-backed end-to-end task:

```bash
task e2e
```

Notes:

- Kind target config lives in `tests/targets/kind.env`; for remote tests, copy
  `tests/targets/remote.env.example` to the ignored local file
  `tests/targets/remote.env`.
- Use `task test:deployed DEPLOY_ENV=kind` for target-specific deployed checks.

### Ingest any MP4 video

```bash
export TAMOSS_API=https://api.tamoss.localtest.me
export TAMOSS_INSECURE_SKIP_TLS_VERIFY=true
export TAMOSS_NAMESPACE="${TAMOSS_NAMESPACE:-tams}"
export TAMOSS_TOKEN="$(kubectl --kubeconfig tams.kubeconfig -n "$TAMOSS_NAMESPACE" \
  get secret tams-api-token \
  -o jsonpath='{.data.TAMOSS_API_TOKEN}' | base64 --decode)"
export TAMOSS_UI_URL=https://app.tamoss.localtest.me
VIDEO=/path/to/video.mp4

# Using the Task runner
task ingest VIDEO="$VIDEO" LABEL="My Video" DURATION=5

# Or use the script directly
./src/scripts/ingest_video.sh "$VIDEO" "My Video" "Optional description" 5
```

Parameters:

- `LABEL` / 2nd argument - Flow label (defaults to filename)
- `DESCRIPTION` / 3rd argument - Flow description (defaults to auto-generated)
- `DURATION` / 4th argument - Segment duration in seconds (defaults to 3)

See `src/scripts/ingest_video.sh` for advanced options (custom `SOURCE_ID`, `BACKEND_ID`,
`FLOW_ID`, etc.)

## Direct API Access

All UI functionality is available via the REST API. The full interactive API reference is
available at <https://api.tamoss.localtest.me/docs> when the stack is running.
Set `TAMOSS_TOKEN` to the generated API token before running direct examples, or
use an OAuth2 access token from the Full profile authentik issuer. Fresh
deployments do not contain flows or segments, so seed one media-backed flow
before running the read and playback examples.

```bash
export TAMOSS_API=https://api.tamoss.localtest.me
export TAMOSS_INSECURE_SKIP_TLS_VERIFY=true
export TAMOSS_UI_URL=https://app.tamoss.localtest.me
export TAMOSS_NAMESPACE="${TAMOSS_NAMESPACE:-tams}"
export TAMOSS_TOKEN="$(kubectl --kubeconfig tams.kubeconfig -n "$TAMOSS_NAMESPACE" \
  get secret tams-api-token \
  -o jsonpath='{.data.TAMOSS_API_TOKEN}' | base64 --decode)"

FLOW_ID="$(uuidgen | tr 'A-Z' 'a-z')"
VIDEO=/path/to/video.mp4

FLOW_ID="$FLOW_ID" \
  ./src/scripts/ingest_video.sh "$VIDEO" "Direct API Example" "" 5

GET_URL_LABEL="$(curl -ks -H "Authorization: Bearer $TAMOSS_TOKEN" \
  "$TAMOSS_API/service/storage-backends" | jq -r '.[0].label')"

OBJECT_ID="$(curl -ksG -H "Authorization: Bearer $TAMOSS_TOKEN" \
  "$TAMOSS_API/flows/$FLOW_ID/segments" | jq -r '.[0].object_id')"
```

### List all flows

```bash
curl -k -H "Authorization: Bearer $TAMOSS_TOKEN" "$TAMOSS_API/flows" | jq
```

### Get flow by ID

```bash
curl -k -H "Authorization: Bearer $TAMOSS_TOKEN" "$TAMOSS_API/flows/$FLOW_ID" | jq
```

### Filter by tag

`/flows` and `/sources` accept BBC dynamic tag parameters. Use
`--data-urlencode` for tag names or values that contain reserved URL
characters.

```bash
curl -ksG -H "Authorization: Bearer $TAMOSS_TOKEN" \
  --data-urlencode "tag.environment=prod,stage" \
  "$TAMOSS_API/flows" | jq

curl -ksG -H "Authorization: Bearer $TAMOSS_TOKEN" \
  --data-urlencode "tag_exists.operator=true" \
  "$TAMOSS_API/sources" | jq

curl -ksG -H "Authorization: Bearer $TAMOSS_TOKEN" \
  --data-urlencode "tag_exists.reviewed=false" \
  "$TAMOSS_API/flows" | jq
```

Object details can filter referenced flows with the `flow_tag.*` variants:

```bash
curl -ksG -H "Authorization: Bearer $TAMOSS_TOKEN" \
  --data-urlencode "flow_tag.environment=prod" \
  --data-urlencode "flow_tag_exists.operator=true" \
  "$TAMOSS_API/objects/$OBJECT_ID" | jq
```

### Get segments for a flow

```bash
curl -ksG -H "Authorization: Bearer $TAMOSS_TOKEN" \
  --data-urlencode "accept_get_urls=$GET_URL_LABEL" \
  --data-urlencode "presigned=true" \
  "$TAMOSS_API/flows/$FLOW_ID/segments" | jq
```

### Fetch a presigned object URL and play

```bash
URL="$(curl -ksG -H "Authorization: Bearer $TAMOSS_TOKEN" \
  --data-urlencode "accept_get_urls=$GET_URL_LABEL" \
  --data-urlencode "presigned=true" \
  "$TAMOSS_API/flows/$FLOW_ID/segments" | jq -r '.[0].get_urls[0].url')"
ffplay -autoexit "$URL"
```

## Resetting the Environment

To reset database and storage, recreate the local stack:

```bash
task down
task up
```

This removes local runtime state, recreates the Kind stack, and reapplies the
deployment defaults.

## Related

- [deployment.md](./deployment.md): Deployment and setup options
- [configuration.md](./configuration.md): Runtime and Helm configuration reference
- [troubleshooting.md](./troubleshooting.md): Common issues and fixes
