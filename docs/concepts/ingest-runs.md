# Ingest Runs

An `IngestRun` is the durable record of one request to ingest approved media
into a `Tamoss` instance. It separates user intent and operational history from
the temporary Kubernetes workload that performs the work.

## Resource and Workload

The public resource is `IngestRun`. After validating its immutable intent, the
operator creates a fixed-purpose
[TAMSin v1.0.0-rc.3](https://github.com/livewyer-ops/tamsin/releases/tag/v1.0.0-rc.3)
Kubernetes `Job` and records the Job's name and UID in `status.jobRef`.

The distinction is deliberate:

- `IngestRun` remains after workload cleanup and records phase, progress,
  attempt lineage, conditions, and verified result metadata.
- The TAMSin `Job` is an operator-owned execution detail with a pinned image,
  bounded resources, a fixed command, and no user-supplied Pod fields.
- Deleting or editing the Job does not edit the request. If the recorded Job
  disappears before the outcome is known, the run fails with
  `IngestJobMissing`; the operator does not replay potentially non-idempotent
  ingest work.

`IngestJobMissing` is a condition reason, and "ingest Job" is a convenient
description of the generated workload. There is no TAMOSS `IngestJob` custom
resource. Users create and inspect `IngestRun` resources, never Jobs as an
alternative ingest API.

## Source Policy Boundary

An `IngestRun` contains one immutable HTTPS or S3 selector. The referenced
`Tamoss` owns the reusable source policy that determines whether that selector
may be read. Credentials belong to a named source and cannot be selected by a
run.

There are three modes:

- `Disabled` starts no new runs and is the production default.
- `PublicHTTPS` permits unnamed public HTTPS selectors on port 443 and also
  permits explicitly named HTTP or S3 sources.
- `Restricted` requires every selector to reference a named source.

S3 always requires a named source. HTTP sources constrain an exact HTTPS
origin and optional path prefixes. S3 sources constrain an exact endpoint,
bucket, and optional key prefixes. Signed URLs, URL user information, query
strings, and fragments are rejected. Private resolved addresses require an
explicit opt-in on that named source.

The controller validates the latest policy before creating a Job. The Job then
records the source name and validation digest and receives only the admitted
selector and matching source client settings. Editing a `Tamoss` does not
alter an active Job. DNS answers and redirect targets can change after the
operator's validation, so this is an application-level admission boundary,
not continuous egress enforcement. `IngestRun` does not add a per-run
`NetworkPolicy`.

The same boundary applies to the destination. A run may select a Ready,
media-purpose `StorageBackend` belonging to the same `Tamoss`; otherwise it
uses the TAMS default backend. Callers cannot submit an arbitrary storage UUID
or credentials.

## Flow Profile Resolution

TAMSin can assign an existing TAMS Flow Profile to each generated essence
stream. An `IngestRun` either supplies a raw external Profile UUID or names a
same-namespace `FlowProfile`. The operator waits for a referenced resource to
be current and Ready, verifies that it targets the same `Tamoss` and matches the
requested essence format, then snapshots its UUID before creating the Job.

This keeps durable ingest history intelligible without making the run a Profile
creation surface. The Console displays the selected resource and UUID but does
not create either `IngestRun` or `FlowProfile` resources.

## Immutable Attempt History

Input, profile, size class, options, output metadata, target instance, and retry
parent are immutable. The only mutable intent is a one-way change from
`desiredState: Running` to `Cancelled`.

Cancellation removes the owned Job and waits for its Pods to terminate before
the run becomes `Cancelled`. A cancelled or completed run cannot be restarted.
A retry is a new `IngestRun` linked to the exact name and UID of a terminal
parent, which preserves both attempts as history.

## Output Intent and Identity

A single-input run can carry constrained human-facing metadata for the Flow
graph produced from that input: `label`, `description`, and ordinary TAMS
tags. TAMOSS translates this intent to TAMSin's
[`--flow-metadata`](https://github.com/livewyer-ops/tamsin/blob/v1.0.0-rc.3/docs/reference/cli.md)
argument. It does not expose arbitrary Flow JSON, technical media overrides,
FFmpeg arguments, identifiers, or TAMSin's wider CLI.

TAMSin applies the metadata to the root and every member Flow it generates.
Output intent requires `options.maxInputs: 1`: metadata for a manifest or
prefix expansion needs a separate per-input template contract rather than an
implicit convention. Tags whose names start with `_tamsin_` remain owned by
TAMSin and are rejected.

For a completed single-input run, the operator records the root Flow, Source,
and up to 16 member Flows from TAMSin's validated event stream. It never scans
TAMS after completion to infer which resources a run created. The Console uses
these identities for read-only links to the resulting catalogue resources.

## Lifecycle

The operator projects these phases:

| Phase | Meaning |
| --- | --- |
| `Pending` | Configuration, approval, readiness, or scheduling prerequisites are not yet satisfied. |
| `Queued` | The TAMSin Job exists and is waiting to run. |
| `Running` | TAMSin is starting, processing media, or terminating after cancellation. |
| `Succeeded` | Every input completed successfully. |
| `PartiallySucceeded` | Execution completed with both successful and failed inputs. |
| `Failed` | The request cannot proceed or the attempt ended without a safe successful outcome. |
| `Cancelled` | Cancellation was requested and the owned workload has terminated. |

TAMSin emits the versioned `tamsin.ingest.events` 2.1 machine event stream. The
operator validates it with TAMSin's published reducer and retains only bounded
counters, stable reasons, attempt identity, output resource identities, and
verified result metadata on the CR. Free-form Pod logs and raw media locators
are not exposed by the Console API.

## Console Boundary

The Console UI provides paginated history, phase filtering, detail, and
one-way cancellation. Viewers can read runs; operators can cancel an active run
when the capability is available. Creation and retry are deliberately outside
the browser boundary. Automation or a Kubernetes principal with explicit CR
permissions must create those resources.

See [Manage ingest runs](../operations/manage-ingest-runs.md) for operational
commands and [IngestRun CR](../reference/ingestrun-cr.md) for the exact fields.
