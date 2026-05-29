# Using TAMOSS

Use the web UI for operational browsing and selected API-backed actions. Use the
BBC TAMS API as the authoritative protocol contract.

## Web UI

After local Kind install, open:

```text
https://app.tamoss.localtest.me
```

The UI can:

- Browse flows, segments, objects, and service state.
- Allocate storage and register uploaded media.
- Select from registered storage backends where supported.
- Inspect deletion requests and runtime health.

## API

Interactive API docs are available at:

```text
https://api.tamoss.localtest.me/docs
```

Set a token for direct examples:

```bash
export TAMOSS_API=https://api.tamoss.localtest.me
export TAMOSS_TOKEN="$(kubectl --kubeconfig tams.kubeconfig -n tams \
  get secret tams-api-token \
  -o jsonpath='{.data.TAMOSS_API_TOKEN}' | base64 --decode)"
```

List service metadata:

```bash
curl -k -H "Authorization: Bearer $TAMOSS_TOKEN" "$TAMOSS_API/service" | jq
```

List storage backends:

```bash
curl -k -H "Authorization: Bearer $TAMOSS_TOKEN" \
  "$TAMOSS_API/service/storage-backends" | jq
```

## Ingest Helper

`task kind:up` creates one tiny playable demo ingest without requiring local media
conversion tools. The demo segment is registered with probe-derived
`object_timerange`, `ts_offset`, `last_duration`, and `key_frame_count`
metadata. Browser-managed ingest also probes finalized MPEG-TS segments before
registering them, so registered segment timeranges come from measured media
duration rather than desired segment length.

`task ingest` is the optional helper for arbitrary local media files, not the
public deployment path:

```bash
task ingest VIDEO=/path/to/video.mp4 LABEL="Example"
```

The arbitrary ingest helper may require local media tooling such as `ffmpeg`
because it segments user-supplied files before uploading them.

Validate the checked-in managed-ingest fixture through containerized media
tooling:

```bash
task test:media:fixtures
```

For deployed confidence, prefer:

```bash
task e2e:deployed PROFILE=local-kind KUBECONFIG=tams.kubeconfig
```

## Presigned URLs

Presigned URLs are temporary credentials. Do not paste complete URLs into public
issues, logs, or documentation.

Browser uploads to external S3-compatible buckets require provider-side CORS for
the TAMOSS UI origin. See [Troubleshooting](operations/troubleshooting.md).

For local Kind, browser uploads use presigned URLs on
`https://s3.tamoss.localtest.me`. Accept or trust the local self-signed TLS
certificate for that S3 origin, not only the app origin.

## Media Deletion

Flow and segment deletion is metadata-first. The worker removes Flow Segment and
Media Object metadata before deleting controlled object bytes, then records
object-store cleanup in the database so failed storage operations can be retried.

Delete request `error.type` values are public-safe. `object_cleanup_failed`
means deleted media is no longer exposed through the API, but controlled bytes
remain queued for retry by the worker.
`delete_request_failed` means metadata deletion failed before cleanup planning
completed; retry the worker and use API logs for the concrete exception detail.
