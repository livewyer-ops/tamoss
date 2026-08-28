---
status: "accepted"
---
# Media Never Transits the Cluster

## Context and Problem Statement

A TAMS store addresses media by time, and clients read and write Flow Segments against object storage.
Something has to serve those bytes, and the choice determines what the API workloads are and how they scale.

TAMOSS targets four deployment profiles, from `multi-server` down to `edge`, which is a single ARM node.
A design that scales with media throughput rather than with request rate would not fit the smaller end of that range.

## Considered Options

* Option 1: Proxy media through the API workload
* Option 2: Issue presigned URLs and let clients read and write the object store directly
* Option 3: Proxy on read and presign on write, so reads can be cached and authorised per request

## Decision Outcome

Chosen Option 2: Issue presigned URLs and let clients read and write the object store directly.

The `ObjectStorage` port (`src/app/tamoss/ports/object_storage.py`) is the whole of the contract, and it moves no bytes: `build_put_request`, `build_get_urls_batch`, `object_metadata`, `copy`, `delete`, and `delete_batch`.
There is no streaming or proxying path anywhere in `src/app/tamoss/api/`.
The API deals in the index and in URLs; the media plane is strictly between the client and the bucket.

**Confidence:** High.
The `edge` profile running on a single ARM node is direct evidence, because a proxying design could not support it.

**Reevaluate if:** a deployment becomes bound by object-store egress cost rather than by CORS configuration, or withdrawing access before a presigned URL expires becomes a requirement.

### Consequences

* API workloads are genuinely stateless and instances are disposable, which is what makes the `edge` profile viable and horizontal scaling of the API a request-rate question only.
* Browser reads depend on the bucket's own CORS configuration, which TAMOSS does not control and cannot fully diagnose from inside the cluster. This is a recurring support cost.
* A presigned URL cannot be withdrawn before its TTL expires. Access is revoked at issue time, not at read time.
* There is no cache or CDN tier to add without reversing this decision. An in-cluster read cache reintroduces the media path to the cluster and should not be considered until a deployment is bound by object-store egress rather than by CORS configuration.
* The index and the bytes live in two systems that are backed up, restored and mutated independently, and nothing reconciles them. That seam is the direct price of this decision, and detecting drift across it needs a reconciliation pass that does not exist today.

## Pros and Cons of the Options

### Option 1: Proxy media through the API workload

* Good, because access can be authorised per request and revoked immediately
* Good, because the object store need not be reachable from clients at all
* Good, because it gives an obvious place to cache
* Bad, because the API must then scale with media throughput rather than request rate, which the `edge` and `single-server` profiles cannot do
* Bad, because it puts a Kubernetes-shaped bottleneck in front of an object store already built to serve bytes at scale

### Option 2: Presigned URLs direct to the object store

* Good, because API workloads stay stateless and cheap
* Good, because throughput is the object store's problem, which is what it is for
* Bad, because browser access depends on bucket CORS we do not own
* Bad, because a URL cannot be revoked before its TTL
* Bad, because it creates a durability seam between the index and the bytes that nothing currently inspects

### Option 3: Proxy on read, presign on write

* Good, because it recovers per-request authorisation and caching for the read path
* Neutral, because writes keep the scaling properties of Option 2
* Bad, because reads are the higher-volume path in a media store, so it takes most of Option 1's cost
* Bad, because it means two media paths to implement, secure, and explain rather than one
