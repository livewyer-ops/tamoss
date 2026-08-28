---
status: "proposed"
---
# Media-Plane Compute on the Ingest Job Pattern

## Context and Problem Statement

[ADR0006](./0006-operator-owned-ingest-jobs.md) established that batch media work runs as operator-owned Kubernetes Jobs, rendered from a declarative resource, with a pinned image the user never names.
That machinery, covering ownership, status, attempt history, garbage collection, and cancellation, is general.
It serves exactly one media workload, TAMSin ingest, but it is already the third hand-rolled Job path in the operator.
`SchemaController` renders migration Jobs and `flowProfileRegistrationJob` renders Profile registration and deletion Jobs, each with its own ownership, status, and cleanup code, and neither reuses the ingest machinery.

A TAMS store accumulates other batch work over media it already holds.
Transcoding a Flow into an additional rendition, generating thumbnails or proxies, extracting technical metadata for indexing, and quality control over stored Segments are all the same shape: read Segments, do work, write results back through the API.

Nothing about the existing machinery is specific to ingest, but nothing about it is reusable either, because `IngestRun` names ingest in its type, its status, and its documentation.
The duplication already visible in the schema and Profile controllers is what that unreusability costs, and a second media workload would add a fourth.
The question is what shape the second workload takes, and it should be answered before there is a second workload rather than after.

This record builds on machinery that arrives with TAMS 8.2 support: on `main` the `IngestRun` and `FlowProfile` resources do not yet exist.
The question is worth settling now precisely because the first workload has not finished landing.

## Considered Options

* Option 1: Extend `IngestRun` with additional actions
* Option 2: One generic `MediaTask` resource parameterised by workload type
* Option 3: A resource per capability, such as `TranscodeRun` or `ThumbnailRun`, over shared internal machinery
* Option 4: Let users submit their own Jobs against a documented contract

## Decision Outcome

No option chosen yet.
This record exists to fix the question before the first new workload forces an answer by accident.

Option 4 is rejected outright: it is Option 1 of [ADR0006](./0006-operator-owned-ingest-jobs.md) under another name, and the reasoning there still holds.

The live choice is between Options 2 and 3, and the evidence that would settle it is how much the second workload actually shares with ingest.
If its inputs, status and failure modes look like `IngestRun`'s, Option 2 is justified.
If it needs its own typed fields to be usable, Option 3 is, and a generic resource would end up carrying a free-form map, the pattern the engineering standards call shoe-horning, and one this project already has a case of in `spec.advanced`.

The cheapest way to find out is to specify one concrete second workload, thumbnail generation being the smallest, in both shapes and compare the resulting CRDs.

**Confidence:** Low, deliberately.
No option has been chosen, and the record exists to hold the question open rather than to settle it.

**Reevaluate if:** nothing yet. Specifying thumbnail generation in both shapes is what would settle it.

### Consequences

* Whichever shape is chosen, the operator remains the only author of Jobs, so [ADR0006](./0006-operator-owned-ingest-jobs.md) is extended rather than reversed.
* Media-plane compute reads and writes object bytes from inside the cluster, which is the first thing to do so since [ADR0001](./0001-media-never-transits-the-cluster.md). That decision constrains the API workloads, not operator-owned batch Jobs, so it is not contradicted, but the reasoning should be restated explicitly wherever it lands, because it looks like a contradiction.
* Any new capability inherits the version-ownership consequence from [ADR0006](./0006-operator-owned-ingest-jobs.md): TAMOSS pins the image, so upgrading the tool becomes a TAMOSS release decision.
* Batch compute competes with the API for cluster resources in the smaller profiles. `edge` may not be able to run it at all, which makes it the first capability that is not available in every profile.

## Pros and Cons of the Options

### Option 1: Extend `IngestRun` with more actions

* Good, because it reuses a resource users already understand, with no new CRD
* Bad, because the name would stop describing the thing, since a transcode of stored media is not an ingest
* Bad, because unrelated fields accumulate on one resource, and validation has to express which combinations are meaningful

### Option 2: A generic `MediaTask` resource

* Good, because one resource and one controller serve every future workload
* Good, because operators learn one status and cancellation model
* Bad, because a resource general enough for every workload tends towards a free-form parameter map, which loses the typing that makes a CRD useful
* Bad, because validation cannot be specific to a workload if the workload is a field value

### Option 3: A resource per capability

* Good, because each resource carries exactly the typed fields its workload needs, and validation is specific
* Good, because capabilities can be added, versioned, and deprecated independently
* Good, because it matches how `IngestRun` and `FlowProfile` already work
* Bad, because it multiplies CRDs and controllers, with shared machinery to factor out and keep factored
* Bad, because each new capability is a CRD change, so the set stays deliberately small

### Option 4: Users submit their own Jobs

* Good, because any workload becomes possible immediately with no operator work
* Bad, because it makes the tool's CLI a public interface, which [ADR0006](./0006-operator-owned-ingest-jobs.md) rejected
* Bad, because it requires granting users workload-creation rights in the instance namespace, weakening [ADR0003](./0003-namespaces-as-the-tenancy-boundary.md)
