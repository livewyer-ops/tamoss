# 0005: Tamsin IngestRun and NDJSON Events

## Status

- The declarative run, Job ownership, cancellation, result, and event
  boundaries are accepted for TAMOSS 8.2.
- The `IngestRun` CRD, approved HTTP input resolver, HTTPS endpoint resolver,
  and operator-owned Tamsin Job path are implemented.
- The Console API and UI expose bounded durable history, detail, session
  capabilities, and audited one-way cancellation. Create and retry remain
  unavailable rather than presenting controls that cannot execute safely.
- Tamsin merged its v1 NDJSON ingest protocol on 9 August 2026 and published
  `0.1.0-rc.2`, the first release carrying the event-based JSON output this
  record depends on. The operator pins its multi-architecture index by digest,
  because `TAMOSS_TAMSIN_IMAGE` accepts only an immutable `sha256` reference
  and an ingest attempt must stay reproducible from its recorded run.
- The input and endpoint resolvers are implemented, so the operator creates
  Tamsin Jobs. Inputs resolve only against `spec.ingest.approvedInputs` on the
  target instance, which binds an opaque id to fixed HTTPS locations; an
  instance that approves nothing resolves nothing. The endpoint is the
  instance's own published API URL, used verbatim, so a plaintext route fails
  the boundary rather than being upgraded — Tamsin carries a full-access bearer
  token and refuses plaintext itself.
- When a Job finishes, the operator reads the Tamsin Pod's event stream once
  through a dedicated `pods/log` grant and records the outcome on status: the
  run identity, last event sequence, and the input and byte counters the
  Console renders. Only bounded machine values are kept; free-form stream text
  is never projected. The stream also disambiguates exit code 4 — no
  successful inputs is `Failed`, a mixture stays `PartiallySucceeded`.
  Collection is enrichment, never a gate: a garbage-collected Pod cannot stop
  a run reaching its terminal phase, and the Console service account still
  receives no log access.
- Live progress during a run is not collected; counters appear when the Job
  finishes. Streaming collection remains open alongside the durable artefact:
  Tamsin 0.1.0-rc.2 writes its journal to the Job's own filesystem and
  publishes neither a digest nor a size for it, so there is nothing for the
  operator to fetch and verify. Digest verification therefore applies only
  once something records a durable result; a run that records none reaches
  its terminal phase on the Job's outcome, backed by Tamsin's own `--verify`
  read-back of every uploaded Object.

## Context

Browser FFmpeg and direct browser registration cannot provide durable,
observable ingest at operational scale. Tamsin already provides deterministic
Flow/Object identities, bounded concurrency, resume, verification, a strict
batch result, and a durable terminal-result journal. It is designed to run as a
Kubernetes Job.

At Tamsin commit `5d6426df0e9892909c986edf4ec86956c755bbfa`, JSON ingest
stdout is the published `tamsin.ingest.events` v1 NDJSON protocol. The durable
journal and batch-result schema are separate recovery and archival contracts.
TAMOSS must consume the upstream event protocol rather than defining a second
near-equivalent envelope or parsing human progress text.

## Accepted Decisions

`IngestRun` is a namespaced TAMOSS custom resource representing one immutable
ingest attempt. A trusted client creates it from a typed request; the current
Console API advertises creation as unavailable. The operator validates the
resource and creates a fixed-purpose Tamsin Job.

Users cannot submit a Job spec, image, command, environment variable, volume,
service account, Secret name, or security context. The operator owns those
fields, pins the Tamsin image by digest, and clamps concurrency, resource, disk,
deadline, retry, grace-period, and retention settings.

An input credential is an approved profile belonging to the referenced
`Tamoss`, never a raw Secret reference supplied by an ingest user. Profile
configuration maps to workload identity or operator-selected Secret keys.
The Job receives only an operator-approved HTTPS TAMS endpoint. Bearer
credentials are never sent to the instance's plaintext ClusterIP service.

## IngestRun Contract

The initial `spec` contains:

| Field | Meaning |
| --- | --- |
| `tamossRef` | `Tamoss` in the same namespace |
| `inputRef` | Opaque staged upload, manifest, approved S3 prefix, or approved HTTP input reference |
| `profile` | Versioned Tamsin ingest profile and treatment |
| `options.storageBackendRef` | Optional same-instance, Ready media `StorageBackend`; the controller resolves its TAMS UUID |
| `credentialProfileRef` | Optional operator-approved input credential profile |
| `options` | Typed verification, dry-run, concurrency, and input-count limits |
| `retryOf` | Optional name and UID of a terminal run |
| `desiredState` | `Running` initially; may change once to `Cancelled` |

Input locators must not contain URL userinfo or credential query parameters.
All fields except the one-way `desiredState` transition are immutable. A retry
creates a new `IngestRun`; it never resets a terminal resource or reuses its
result location.

Status remains bounded and contains no per-input array:

```yaml
status:
  observedGeneration: 1
  phase: Running
  jobRef: {name: ingest-abc, uid: "..."}
  tamsinRunId: "..."
  attempt: 1
  startedAt: "..."
  progress:
    inputsTotal: 1000
    inputsCompleted: 42
    inputsSucceeded: 41
    inputsFailed: 1
    bytesUploaded: 123456
  lastEventSequence: 93
  resultRef:
    key: "..."
    sha256: "..."
    size: 1234
    mediaType: application/json
    verified: true
  conditions: []
```

`phase` is one of `Pending`, `Queued`, `Running`, `Succeeded`,
`PartiallySucceeded`, `Failed`, or `Cancelled`. Conditions use stable reason
codes for acceptance, scheduling, progress, result availability, event gaps,
and failure. Status updates are coalesced; the collector does not write to the
Kubernetes API for every progress event.

## Input and Artefact Flow

For local files, the Console API creates a short-lived multipart staging grant.
The browser uploads directly to the staging object store, then creates an
`IngestRun` using the opaque staged-object reference. The Console API does not
proxy media bytes. Incomplete staging uploads have an enforced lifecycle.

Approved S3, HTTP, and manifest inputs are read directly by the Job. The
resolver returns at most 32 bounded top-level selectors; manifests and S3
prefixes carry large input sets so thousands of URLs never become Kubernetes
Job arguments. Large input lists live in staging or managed object storage,
never in the CRD or Pod spec.

The complete event stream, Tamsin journal, and final batch result are persisted
outside the Pod in operator-managed artefact storage. `resultRef` contains an
opaque key, digest, size, media type, and collector-set verification state,
never a presigned URL. A completed Job does not become `Succeeded` or
`PartiallySucceeded` until this durable result passes digest and size
verification. Jobs may be deleted by TTL after collection; `IngestRun` status
and artifacts follow a separate retention policy.

An operator-side event collector reads only operator-owned ingest Pod stdout,
validates the Job and `IngestRun` owner UIDs, and updates status. The Console
API does not expose or consume generic Pod logs. If collection is a separate
workload, its service account receives `get` on `pods/log` but no exec, attach,
or workload-write permission.

## Tamsin Event Contract

`tamsin ingest --format json` writes one UTF-8 event per stdout line.
Diagnostics remain on stderr; stdout contains no progress text or final batch
document. The upstream envelope is:

```json
{
  "protocol": "tamsin.ingest.events",
  "protocol_version": "1.0",
  "type": "progress.snapshot",
  "seq": 17,
  "run_id": "4f08f19e-9ab5-44a5-9801-f97a342542e4",
  "emitted_at": "2026-08-09T12:00:00Z",
  "elapsed_ms": 1842,
  "scope": {"input_index": 0},
  "payload": {}
}
```

`hello` is sequence zero. `seq` is globally contiguous for a run, including
interleaved inputs, and `run.finished` is the last graceful record. Its exit
code must match the container status; EOF without it is abnormal rather than a
success inference. A Kubernetes Job retry is a new Tamsin invocation and
`run_id`; the collector records that attempt separately.

The v1 protocol is deliberately forward-compatible within its major version.
TAMOSS uses Tamsin's public Go `ingestevent` decoder and reducer from the same
module revision as the promoted image, accepts the compatible minor versions
that decoder supports, and rejects another major with
`EventContractUnsupported`. It does not make the upstream envelope artificially
strict or branch on display messages.

The initial event types are:

| Event | Required payload |
| --- | --- |
| `hello`, `run.started` | producer capabilities and resolved run settings |
| `input.declared`, `manifest.finished`, `input.started` | atomic input selection and dispatch |
| `flow.planned`, `progress.snapshot` | planned graph and cumulative store/verify progress |
| `retry.scheduled`, `diagnostic`, `run.cancellation_requested` | bounded operational state and safe failure codes |
| `object.result`, `flow.result`, `input.finished` | terminal resource and input outcomes |
| `run.finished` | final outcome, intended exit code, counts, bytes, retry, and recovery totals |

`progress.snapshot` is cumulative and coalescible. Its per-input `revision`
increases across store and verify phases. The UI does not derive a percentage
until `totals_final` is true. Tamsin's contract supplies sanitised locators and
closed failure codes; TAMOSS still allow-lists the fields it projects and never
copies raw stderr into status.

The event stream is the live machine contract. Tamsin's durable journal and
strict batch-result schema remain separate recovery and archival inputs. The
collector persists accepted events before advancing its checkpoint and builds
one bounded `IngestRun.status` projection plus a digest-verified external
artefact; it does not recreate a competing event model.

## Cancellation, Retry, and Failure

Changing `desiredState` to `Cancelled` stops Job retries and requests graceful
Pod termination. Tamsin receives SIGTERM and retains its integrity cleanup
window. The operator marks `Cancelled` only after termination is observed; a
cleanup failure is retained as a condition and result, not hidden by the user
action.

The Console cancellation endpoint requires an operator-capable authenticated
session, exact same-origin proof, the run UID, and its current resource revision.
It performs only the one-way desired-state patch, handles a repeated request as
an idempotent replay, and emits a bounded audit record without request bodies or
artefact data.

`run.finished` maps to `Succeeded` when every input succeeds,
`PartiallySucceeded` when execution completes with terminal input failures, and
`Failed` for a run-wide failure. An interrupted run maps to `Cancelled` only
when cancellation was requested.

Pod loss, duplicate log delivery, collector restart, missing terminal events,
and artefact upload failure all have explicit conditions. Tamsin does not write
`IngestRun.status` directly.

Every run must reach a terminal phase. Waiting indefinitely for evidence is not
a safe resting state: it blocks retries, which require a terminal parent, and
keeps the run in the Console's active set. Three bounds enforce this.

A finished Job whose digest-verified result has not appeared within the
observation deadline fails with `ResultVerificationTimeout` rather than polling
until the Job's TTL removes it. The same deadline applies when a failed Job's
Pod is garbage collected before its exit code is read: the run fails with the
Job's own message, losing only the partial-success refinement. Success is never
claimed without verification.

A run whose recorded Job has gone fails with `IngestJobMissing`. Tamsin ingest
is not idempotent, so the operator does not replay it, and the outcome cannot be
confirmed. Absence is checked against the API server before the run is failed,
so a lagging informer cache cannot end a healthy run.

Deleting the target `Tamoss` while a Job runs deletes that Job and fails the run.
The Job is owned by the `IngestRun`, not the instance, so nothing else would stop
it uploading to a deleted endpoint until its active deadline expired.

Instance readiness gates admit new work only. Once an attempt has a Job, its
phase is tracked from that Job alone, so a transient readiness dip neither
regresses a running attempt to `Pending` nor hides the Job's completion.

## Remaining Validation

- Keep the immutable Tamsin image, `ingestevent` module, and JSON Schema pinned
  to the same promoted revision.
- Fixture-test protocol compatibility, redaction, stdout/stderr separation,
  `hello`, sequence gaps, compatible minors, unsupported majors, incomplete
  EOF, and `run.finished`/container exit-code agreement.
- Validate the generated `IngestRun` CRD in envtest, including immutable
  fields, one-way cancellation, same-instance references, and bounded status.
- Prove an ingest user cannot select a Secret, service account, image, command,
  volume, namespace, or unapproved network credential.
- Resolve a trusted HTTPS ingest endpoint, including internal CA distribution
  and hostname verification, and prove bearer credentials cannot be sent over
  plaintext HTTP.
- Test graceful cancellation, hard Pod loss, Job retry, collector restart,
  duplicates, gaps, unsupported schema versions, partial success, stranded
  verification, and artefact failure.
- Run at least 10,000 inputs through one staged manifest or approved prefix
  while keeping Job arguments, CR status, event queues, and Kubernetes API
  writes bounded.
- Verify artefact digests and retention, Job TTL cleanup, staging multipart
  cleanup, and retry lineage.
- Define and test hibernation coordination. Hibernation must either reject an
  instance with active runs or cancel them and wait for terminal, verified
  artifacts before the database snapshot begins; Jobs must not retry against a
  scaled-down API.
- Complete end-to-end Kind ingest and repeat it with Authentik and real TLS on
  `cnm-tamoss-1`.

## References

- [Public IngestRun explanation](../../concepts/ingest-runs.md)
- [Manage Ingest Runs](../../operations/manage-ingest-runs.md)
- [IngestRun CR reference](../../reference/ingestrun-cr.md)

- [Tamsin result contract](https://github.com/livewyer-ops/tamsin/blob/main/docs/reference/result-contract.md)
- [Tamsin ingest event schema](https://github.com/livewyer-ops/tamsin/blob/main/contracts/tamsin/ingest-events-v1.json)
- [Tamsin input expansion](https://github.com/livewyer-ops/tamsin/blob/main/docs/reference/inputs.md)
- [Tamsin Kubernetes Job guidance](https://github.com/livewyer-ops/tamsin/blob/main/docs/how-to/run-as-a-kubernetes-job.md)
- [Kubernetes Job lifecycle](https://kubernetes.io/docs/concepts/workloads/controllers/job/)
