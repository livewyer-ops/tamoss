---
status: "accepted"
---
# PostgreSQL as Both Index and Work Queue

## Context and Problem Statement

TAMOSS performs work outside the request path: delivering webhooks, processing deletion requests, collecting unreferenced objects, sweeping stale allocations, and copying objects between storage backends.
That work must survive a crash, must not be performed twice concurrently, and must be recoverable when a worker dies mid-task.

PostgreSQL is already a hard dependency, because it holds the index that gives the media meaning.
The question is whether asynchronous work needs a second piece of infrastructure alongside it.

## Considered Options

* Option 1: Add a message broker
* Option 2: Claim work from PostgreSQL tables under expiring leases
* Option 3: Model each unit of work as a Kubernetes Job owned by the operator

## Decision Outcome

Chosen Option 2: Claim work from PostgreSQL tables under expiring leases.

Four claimed queues sit behind four tables: `tamoss_webhook_deliveries`, `tamoss_delete_requests`, `tamoss_object_cleanups`, and `tamoss_object_copies` (`src/app/tamoss/db/migrations/assets/schema.sql`).
Alongside them sits a stale-allocation sweeper that produces cleanup work rather than draining a queue.
Each claimed row is taken under a `worker_id` with a 300s lease, which is what makes a crashed worker's work recoverable and additional replicas safe.

**Confidence:** Medium.
The substrate is right for the scale the product targets today, but the ceiling has never been measured.

**Reevaluate if:** claim scans start showing in query timings, or webhook delivery latency becomes a user-visible complaint.

### Consequences

* The runtime dependency count stays at one. There is no broker to install, secure, back up, upgrade, or document in any of the four profiles.
* Queue state is captured by the same backup and restore procedure as the index, rather than needing its own.
* Latency has a floor. The worker sleeps `worker_poll_interval_seconds` after any pass that processed nothing (`src/app/tamoss/worker.py`), so work queued just after an idle pass waits that long to be noticed. `LISTEN`/`NOTIFY` on insert would remove the floor without adding infrastructure, and is the first thing to reach for if delivery latency becomes a complaint.
* The tables are unbounded by construction. `purge_finished_queue_records` exists for that reason and says so: "Webhook deliveries accumulate one row per event per webhook; without a purge the queue tables grow without bound and their claim scans degrade" (`worker.py`). The retention purge keeps them bounded in practice; the ceiling is worth re-measuring when claim scans appear in query timings.
* Queue throughput is bound to database capacity, so scaling ingest and scaling asynchronous work are the same scaling problem.

## Pros and Cons of the Options

### Option 1: Add a message broker

* Good, because delivery is push rather than poll, so there is no latency floor
* Good, because queue load is isolated from the database serving the API
* Good, because brokers offer mature tooling for inspection, replay, and dead-lettering
* Bad, because it is a component to install, secure, back up, upgrade, and document in every profile, including a single ARM node
* Bad, because queue state then lives outside the backup and restore procedure the index already needs, adding a second consistency seam on top of the one in [ADR0001](./0001-media-never-transits-the-cluster.md)

### Option 2: Claim from PostgreSQL under expiring leases

* Good, because it adds no runtime dependency
* Good, because at-least-once delivery and crash recovery come from durability guarantees the index already requires
* Good, because queue state is backed up and restored with everything else
* Good, because claim-with-lease makes horizontal worker scaling safe without further work
* Bad, because polling sets a floor on delivery latency
* Bad, because queue tables grow without bound and need an explicit retention purge
* Bad, because queue throughput competes with API load for database capacity

### Option 3: A Kubernetes Job per unit of work

* Good, because it reuses machinery the operator already has for ingest
* Good, because retries, backoff, and cleanup are Kubernetes concerns rather than ours
* Bad, because per-webhook-delivery Job churn is far beyond what the API server and scheduler are for
* Bad, because it would make the application Kubernetes-aware, contradicting [ADR0005](./0005-kubernetes-agnostic-api.md)
