# Ingest Runs

An `IngestRun` is the durable record of one request to ingest approved media
into a `Tamoss` instance. It separates user intent and operational history from
the temporary Kubernetes workload that performs the work.

## Resource and Workload

The public resource is `IngestRun`. After validating its immutable intent, the
operator creates a fixed-purpose [TAMSin](https://github.com/livewyer-ops/tamsin)
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

## Approved Input Boundary

An `IngestRun` contains an opaque `inputRef`, not a URL, Secret name, signed
locator, or credential. The referenced `Tamoss` resource owns the mapping from
that ID to approved media locations.

The current resolver supports `ApprovedHTTP` entries declared in
`Tamoss.spec.ingest.approvedInputs`. Those entries contain fixed HTTPS URLs
without embedded credentials, query strings, or fragments. An instance with no
approved inputs resolves no media. Other input kinds remain reserved by the CRD
until their staging and credential boundaries are implemented.

The same boundary applies to the destination. A run may select a Ready,
media-purpose `StorageBackend` belonging to the same `Tamoss`; otherwise it
uses the TAMS default backend. Callers cannot submit an arbitrary storage UUID
or credentials.

## Immutable Attempt History

Input, profile, size class, options, credential profile, target instance, and
retry parent are immutable. The only mutable intent is a one-way change from
`desiredState: Running` to `Cancelled`.

Cancellation removes the owned Job and waits for its Pods to terminate before
the run becomes `Cancelled`. A cancelled or completed run cannot be restarted.
A retry is a new `IngestRun` linked to the exact name and UID of a terminal
parent, which preserves both attempts as history.

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

TAMSin emits a versioned machine event stream. The operator retains only
bounded counters, stable reasons, attempt identity, and verified result
metadata on the CR. Free-form Pod logs and raw media locators are not exposed by
the Console API.

## Console Boundary

The Console UI provides paginated history, phase filtering, detail, and
one-way cancellation. Viewers can read runs; operators can cancel an active run
when the capability is available. Console creation and retry controls are
currently unavailable, so automation or a Kubernetes principal with explicit
CR permissions must create those resources.

See [Manage ingest runs](../operations/manage-ingest-runs.md) for operational
commands and [IngestRun CR](../reference/ingestrun-cr.md) for the exact fields.
The internal [TAMSin ingest design record](../development/ui-overhaul/0005-tamsin-ingest-runs.md)
preserves architecture decisions and future work.
