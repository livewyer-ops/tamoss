# Using TAMOSS

Use the Console to browse media and inspect instance operations. Use the BBC
TAMS API for media writes and Kubernetes resources for managed ingest.

## Web UI

After local [Kind](https://kind.sigs.k8s.io/) install, open:

```text
https://app.tamoss.localtest.me
```

The UI supports the following actions:

- Browse flows, segments, objects, and service state.
- Browse Flow Profiles, follow Profile-backed Flows, and filter by Flow status.
- Inspect initialisation Objects linked to fragmented media.
- Inspect registered storage backends and Object locations.
- Inspect deletion requests and runtime health.
- Inspect paginated `IngestRun` history and cancel active runs when authorised.

See [Manage Ingest Runs](operations/manage-ingest-runs.md) for the Console and
`kubectl` workflow. The current Console does not create or retry runs.

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

`task kind:up` creates one small playable demo ingest without requiring local media
conversion tools. Set `KIND_DEMO_INGEST=false` when you need a clean validation
target with no seeded flow/source. The demo segment is registered with probe-derived
`object_timerange`, `ts_offset`, `last_duration`, and `key_frame_count`
metadata. For managed ingest, create an `IngestRun`; the operator starts the
pinned TAMSin workload and records its progress and results. See
[Manage Ingest Runs](operations/manage-ingest-runs.md).

`task ingest` is the optional helper for arbitrary local media files, not the
public deployment path and not an `IngestRun` producer:

```bash
task ingest VIDEO=/path/to/video.mp4 LABEL="Example"
```

The arbitrary ingest helper may require local media tooling such as `ffmpeg`
because it segments user-supplied files before uploading them.

Validate the checked-in managed-ingest fixture through containerised media
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

Playback against external S3-compatible buckets requires provider-side CORS
for every browser origin that reads presigned Object URLs. API CORS is
configured separately on the `Tamoss` resource. See
[Troubleshooting](operations/troubleshooting.md).

For local Kind, playback reads media from `https://s3.tamoss.localtest.me`.
Trust the local self-signed TLS certificate for both the app and S3 origins.

## Media Deletion

Flow and segment deletion is metadata-first. The worker removes Flow Segment and
Media Object metadata before deleting controlled object bytes, then records
object-store cleanup in the database so failed storage operations can be retried.

Delete request `error.type` values are public-safe. `object_cleanup_failed`
means deleted media is no longer exposed through the API, but controlled bytes
remain queued for retry by the worker.
`delete_request_failed` means metadata deletion failed before cleanup planning
completed; retry the worker and use API logs for the concrete exception detail.
