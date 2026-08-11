# IngestRun CR Reference

`IngestRun` declares one durable attempt to ingest approved media into a
`Tamoss` instance. The operator validates the request and owns the resulting
Tamsin Kubernetes Job.

Group: `tamoss.livewyer.io`

Version: `v1alpha1`

Kind: `IngestRun`

Scope: `Namespaced`

Short name: `ir`

## Approved Input and Run Example

Declare the input allow-list on the target `Tamoss`:

```yaml
apiVersion: tamoss.livewyer.io/v1alpha1
kind: Tamoss
metadata:
  name: tamoss-kind
  namespace: tams
spec:
  profile: local-kind
  ingest:
    approvedInputs:
      - id: example-programme
        kind: ApprovedHTTP
        urls:
          - https://media.example.com/programmes/example.mp4
```

Reference only its opaque ID from the run:

```yaml
apiVersion: tamoss.livewyer.io/v1alpha1
kind: IngestRun
metadata:
  name: example-programme-001
  namespace: tams
spec:
  tamossRef:
    name: tamoss-kind
  inputRef:
    kind: ApprovedHTTP
    id: example-programme
  profile: editorial@1
  sizeClass: standard
  options:
    verify: true
    maxInputs: 1000
  desiredState: Running
```

Only `ApprovedHTTP` is resolved by the current operator. Approved URLs must use
HTTPS and cannot contain user information, query strings, or fragments.

## Spec Fields

| Field | Purpose |
| --- | --- |
| `.spec.tamossRef.name` | Target `Tamoss` in the same namespace. Required and immutable. |
| `.spec.inputRef.kind` | Opaque input kind. The CRD reserves `StagedObject`, `Manifest`, `ApprovedS3`, and `ApprovedHTTP`; only `ApprovedHTTP` currently resolves. |
| `.spec.inputRef.id` | Approved input ID, 1-128 characters matching `^[A-Za-z0-9][A-Za-z0-9._-]*$`. Required and immutable. |
| `.spec.profile` | Versioned Tamsin treatment: `preserve@1`, `editorial@1`, or `streaming-ts@1`. Defaults to `editorial@1`; immutable. |
| `.spec.sizeClass` | Operator-owned resource class: `small`, `standard`, or `large`. Defaults to `standard`; immutable. |
| `.spec.options.storageBackendRef.name` | Optional Ready, media-purpose `StorageBackend` belonging to the target instance. Unset uses the default TAMS backend. |
| `.spec.options.verify` | Verify uploaded Object bytes. Defaults to `true`. |
| `.spec.options.dryRun` | Render and plan without changing TAMS. Defaults to `false`. |
| `.spec.options.maxInputs` | Maximum expanded inputs, from 1 to 10,000. Defaults to 1,000. |
| `.spec.options.concurrency` | Parallel inputs, from 0 to 32. Zero selects the size-class default. |
| `.spec.credentialProfileRef.name` | Reserved operator-approved credential profile. The current operator has no credential profile resolver, so setting it keeps the run `Pending`. |
| `.spec.desiredState` | `Running` or `Cancelled`. Defaults to `Running`; cancellation is a one-way transition. |
| `.spec.retryOf.name` | Name of a terminal parent run. |
| `.spec.retryOf.uid` | Exact parent UID, preventing a replacement object with the same name from becoming the retry source. |

All spec fields except the one-way `desiredState` transition are immutable. A
retry must preserve the parent's target, input, profile, size class, options,
and credential profile.

## Status Fields

| Field | Purpose |
| --- | --- |
| `.status.observedGeneration` | Resource generation observed by the controller. |
| `.status.phase` | `Pending`, `Queued`, `Running`, `Succeeded`, `PartiallySucceeded`, `Failed`, or `Cancelled`. |
| `.status.conditions` | At most 16 Kubernetes conditions with bounded, stable reason codes. |
| `.status.jobRef` | Name and UID of the operator-owned Tamsin Job. |
| `.status.tamsinRunId` | Tamsin attempt identity observed from its event stream. |
| `.status.attempt` | Attempt number, including validated retry lineage. |
| `.status.progress` | Bounded input totals, completion counts, failure counts, and uploaded bytes. |
| `.status.lastEventSequence` | Last accepted Tamsin event sequence. |
| `.status.resultRef` | Durable key, SHA-256 digest, size, media type, and verification state. The Console projection omits the private key. |
| `.status.startedAt`, `.status.completedAt` | Attempt start and completion timestamps. |

## Tamoss Approved Input Fields

| Field | Purpose |
| --- | --- |
| `.spec.ingest.approvedInputs` | Up to 16 approved input definitions. Empty is fail-closed and resolves no media. |
| `.spec.ingest.approvedInputs[].id` | Opaque ID referenced by an `IngestRun`. |
| `.spec.ingest.approvedInputs[].kind` | Currently `ApprovedHTTP`. |
| `.spec.ingest.approvedInputs[].urls` | One to 16 fixed HTTPS media URLs. Each value has a maximum length of 2,048 characters. |

## Ownership and Access

The operator alone creates and mutates the Tamsin Job. A user-supplied
`IngestRun` cannot choose its image, command, environment, volumes, service
account, Pod security context, Secret, or arbitrary network destination.

The Console supports paginated list and detail reads. Its create and retry
capabilities are currently unavailable. An authenticated user with the
`operator` role can request one-way cancellation when the Console backend is
available.

See [Ingest Runs](../concepts/ingest-runs.md) for the model and
[Manage Ingest Runs](../operations/manage-ingest-runs.md) for operational
commands. The canonical CRD under `operator/config/crd/bases/` is the
exhaustive schema source.
