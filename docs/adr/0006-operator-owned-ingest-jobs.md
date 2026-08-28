---
status: "accepted"
---
# The Operator Is the Only Author of Ingest Jobs

## Context and Problem Statement

Ingesting media into TAMOSS means running [TAMSin](https://github.com/livewyer-ops/tamsin), a separate LiveWyer tool that transcodes source media and registers the resulting Flows and Segments.
It is a batch workload with a finite lifetime, which is a Kubernetes Job.

The question is who writes that Job, because whoever does defines the interface users depend on.

This decision is recorded ahead of the code reaching `main`.
The `IngestRun` and `FlowProfile` resources, the controller and the concept documentation referenced below arrive with TAMS 8.2 support; the file references in this record are to the `8.2-preview` branch and do not resolve on `main` yet.

## Considered Options

* Option 1: Users author Jobs themselves against a documented TAMSin image
* Option 2: The operator owns Jobs, rendered from `IngestRun` and `FlowProfile`
* Option 3: The worker performs ingest in-process

## Decision Outcome

Chosen Option 2: The operator owns Jobs, rendered from `IngestRun` and `FlowProfile`.

`docs/concepts/ingest-runs.md` states it directly, calling the TAMSin Job "an operator-owned execution detail with a pinned image", and the controllers construct every Job themselves (`operator/internal/controller/flowprofile_controller.go`).
Users declare intent and never author a Job.

**Confidence:** Medium.
The pattern is right, but it has served one workload, so the cost of modelling each capability in the CRD is not yet known.

**Reevaluate if:** CRD modelling becomes the bottleneck for delivering ingest capability that TAMSin already supports.

### Consequences

* TAMSin stays an implementation detail rather than a public interface, so its CLI can change without breaking users.
* Ingest is under the same ownership, status and garbage-collection machinery as everything else the operator renders, rather than having a second lifecycle.
* Every TAMSin capability must be modelled in the CRD before it is reachable. Users cannot pass through FFmpeg arguments or wider CLI flags (`docs/concepts/ingest-runs.md`).
* TAMOSS owns the TAMSin version, so upgrading it is a TAMOSS release decision rather than a user's.
* The Job machinery is general and currently serves one purpose. Further media-plane compute, such as transcode, thumbnailing, indexing, and quality control, would reuse this pattern rather than introduce another.

## Pros and Cons of the Options

### Option 1: Users author Jobs

* Good, because every TAMSin capability is immediately reachable without CRD work
* Good, because there is nothing for the operator to implement
* Bad, because TAMSin's CLI becomes a public interface we cannot change
* Bad, because ingest state lives outside the operator, so nothing can report it or clean it up
* Bad, because it requires giving users permission to create workloads in the instance namespace, weakening [ADR0003](./0003-namespaces-as-the-tenancy-boundary.md)

### Option 2: The operator owns Jobs

* Good, because TAMSin remains an implementation detail with a pinned, tested version
* Good, because ingest reuses existing ownership, status, and cleanup machinery
* Good, because intent is declarative and converges like everything else
* Bad, because each capability must be modelled in the CRD before anyone can use it
* Bad, because the TAMSin version becomes a TAMOSS release concern

### Option 3: Ingest in the worker process

* Good, because it needs no Job machinery at all
* Bad, because a long transcode inside the polling loop would block every other queue, which [ADR0002](./0002-postgresql-as-index-and-work-queue.md) already shares one process for
* Bad, because ingest resource requirements differ from the worker's by orders of magnitude, and they would have to share a pod spec
* Bad, because a failed transcode would take out webhook delivery and object cleanup with it
