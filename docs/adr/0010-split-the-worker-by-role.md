---
status: "proposed"
---
# Split the Worker by Role and Scale It on Queue Depth

## Context and Problem Statement

All asynchronous work runs in a single sequential loop.
`drain_once` (`src/app/tamoss/worker.py`) carries a comment stating the position plainly, "Keep one worker process sequential; run more replicas or split loops if queue isolation becomes necessary", and behind it `drain_delete_requests` (`worker.py`) drains three claimed queues in order and runs the stale-allocation sweeper that produces work for a fourth.
Webhook delivery is the remaining claimed queue.
Two booleans gate all of it: `TAMOSS_WORKER_ENABLE_DELETE` and `TAMOSS_WORKER_ENABLE_WEBHOOK` (`src/app/tamoss/settings.py`).

These roles have little in common.
Webhook delivery is latency-sensitive and bound by outbound HTTP to third-party endpoints.
Object cleanup is destructive and bound by the object store.
Cross-backend copy is bandwidth-bound and long-running.
They share a process, a poll interval, a lease duration, and a replica count.

Two problems follow.

The gating is coarser than the restore procedure needs.
Setting `TAMOSS_WORKER_ENABLE_DELETE=false` does not disable deletion alone: it also stops cross-backend object copies and stale-allocation queueing, because they sit inside the same drain.
An operator following `docs/operations/backup-restore.md` to protect media therefore halts replication as a side effect, with no way to express the narrower intent.
That procedure currently disables the whole worker, which is blunter still: it also pauses webhook delivery.

There is also no autoscaling signal.
`renderHPAs` (`operator/internal/controller/workload_renderer/hpa.go`) emits a HorizontalPodAutoscaler for every other workload the operator renders but never for the worker, although `AutoscalingSpec` is reachable on `WorkerComponentSpec` through the inlined `WorkloadCommonSpec`.
Correctness is not the obstacle: every queue is claimed under an expiring lease keyed by a distinct `worker_id` (`settings.py`), so additional replicas are already safe today.

What the cluster can measure is the obstacle.
`deploy/platform/dependencies.yaml` installs cert-manager, Traefik, Authentik, CloudNativePG, and the RustFS operator, and no profile installs metrics-server or Prometheus.
The `ServiceMonitor` and alert rules under `operator/config/prometheus` are an overlay that renders only where Prometheus Operator CRDs already exist (`docs/operations/day-2.md`).
The HPAs rendered today therefore already assume a metrics pipeline the install does not provide.

## Considered Options

Topology:

* Option 1a: Keep one worker Deployment, and add a typed `spec.worker.enableDelete` field
* Option 1b: Render one Deployment per worker role from the `Tamoss` resource

Scaling signal:

* Option 2a: No autoscaling; operators set `replicaCount` themselves
* Option 2b: Render an HPA for the worker on CPU utilisation, as for the other components
* Option 2c: Export queue depth as a metric and drive an HPA through custom metrics
* Option 2d: Drive scaling from KEDA's PostgreSQL scaler
* Option 2e: Have the operator poll queue depth and set worker replicas directly

## Decision Outcome

Preferred options, not yet agreed:

* Option 1b: Render one Deployment per worker role
* Option 2e: Have the operator poll queue depth and set worker replicas directly

Option 1b subsumes the typed field Option 1a would add, because a role that is not rendered is a role that is not running, so a restored instance can be brought up with deletion absent while webhook delivery and replication continue, and an operator has to actively add the role back rather than remember to.

Option 2c was preferred until the pipeline it assumes was checked, and it does not survive the check.
The HorizontalPodAutoscaler has no native custom-metrics path: `custom.metrics.k8s.io` and `external.metrics.k8s.io` are aggregated APIs that Kubernetes ships no implementation of, and metrics-server serves `metrics.k8s.io` only.
Serving queue depth to an HPA therefore means running an adapter, and prometheus-adapter backed by a Prometheus is the usual shape rather than the only one, since kube-metrics-adapter can scrape the gauge from the pods directly and each cloud provider ships its own.
Which adapter is chosen changes the component count but not the conclusion, because the adapter process is small enough for any profile while a metrics store to back it is not, and the same choice would have to hold on a 4 GB ARM node as on `multi-server`.
2d costs KEDA alone, because KEDA is itself the adapter.
The [ADR0002](./0002-postgresql-as-index-and-work-queue.md) argument for holding the dependency count down, which was written against 2d, tells more strongly against 2c.

Option 2e is preferred because it is the only option that satisfies that argument outright.
The operator already reconciles the worker Deployments, so polling a depth gauge and setting `.spec.replicas` adds nothing to any profile, and reading the gauge over HTTP keeps the operator out of the database as [ADR0005](./0005-kubernetes-agnostic-api.md) requires.
Option 2d stays the documented opt-in for clusters already running KEDA, where it is the better answer, and it is the only route to scale-to-zero on `edge`.

The two decisions are independent and can land separately.
The split is worth doing on its own for the quiescing benefit alone.

**Confidence:** Medium.
The role split is well evidenced, and the argument against 2c is now checked against the platform bill of materials rather than inherited, but no scaling loop has been run against a real backlog, so the poll interval and the depth-to-replica function are unmeasured.

**Reevaluate if:** the hand-written scaling loop starts acquiring the behaviours `HorizontalPodAutoscalerBehavior` already defines, such as stabilisation windows and scale-down rate limits, which would make 2d the cheaper place to put them.

### Consequences

* Deletion, webhook delivery, and copy can be scaled, quiesced, and network-policed independently, which is what the restore procedure needs.
* The CRD grows a role dimension under `spec.worker`, and the operator renders more objects per instance.
* More Deployments means more pods at idle. The `edge` profile, which targets a single ARM node, may need roles co-scheduled or some left at zero replicas.
* Queue depth becomes part of the supported interface, exposed by the worker over HTTP and consumed by the operator.
* The operator gains a polling loop, write access to `.spec.replicas` on the Deployments it renders, and a NetworkPolicy edge to the worker's metrics endpoint.
* No HPA is rendered for the worker, so operator-driven scaling and a user-supplied HPA are mutually exclusive on those Deployments.
* `TAMOSS_WORKER_ENABLE_DELETE` and `TAMOSS_WORKER_ENABLE_WEBHOOK` become redundant once roles are rendered separately, and should be removed rather than left as a second way to express the same thing.

## Pros and Cons of the Options

### Option 1a: One Deployment, typed `enableDelete` field

* Good, because it is a small change that makes the restore procedure expressible in the CRD
* Good, because it changes no deployment topology, so no profile is affected
* Bad, because the gate still covers copy and stale-allocation queueing along with deletion, so the narrower intent still cannot be expressed
* Bad, because the roles keep sharing a replica count, so none of them can be scaled for its own bottleneck

### Option 1b: One Deployment per role

* Good, because each role gets its own replica count, autoscaling, resources, and NetworkPolicy
* Good, because quiescing a role is expressed by not rendering it, which is a state an operator must actively leave
* Good, because a destructive role can be network-policed away from endpoints it has no business reaching
* Bad, because it multiplies rendered objects and idle pods, which the smallest profile feels
* Bad, because it is a larger operator change than a single typed field

### Option 2a: No autoscaling

* Good, because it adds nothing and matches how the worker runs today
* Neutral, because claim-with-lease already makes manually raising replicas safe
* Bad, because backlog growth stays invisible until it is someone's incident

### Option 2b: HPA on CPU

* Good, because it reuses exactly the machinery already rendered for the other three components
* Good, because it needs no new metric
* Bad, because the roles are I/O-bound. A worker saturated waiting on outbound webhook HTTP or object-store deletes shows low CPU while its queues grow, so the signal does not track the backlog

### Option 2c: HPA on exported queue depth

* Good, because queue depth is the quantity that actually represents the backlog
* Bad, because the HPA has no native custom-metrics path, so `custom.metrics.k8s.io` has to be served by an adapter that Kubernetes does not ship and metrics-server does not provide
* Bad, because it is therefore the exported gauge, an adapter, an adapter rule per queue, and in the common case a metrics store behind it, none of which is in `deploy/platform/dependencies.yaml`
* Bad, because the profiles do not agree on what they can afford. An adapter alone fits anywhere, but the Prometheus that usually backs it does not fit the `edge` node beside CNPG and RustFS, so autoscaling would work differently per profile or not at all on the smallest one
* Bad, because an `APIService` is a cluster-scoped singleton per group, so TAMOSS cannot ship its own adapter without colliding with whichever one a `multi-server` cluster already runs
* Bad, because the depth metric becomes a supported interface that has to keep working

### Option 2d: KEDA PostgreSQL scaler

* Good, because it reads queue depth directly from the database with no metric to export
* Good, because KEDA is itself the metrics adapter and the HPA author, serving `external.metrics.k8s.io` from its metrics apiserver and generating the HorizontalPodAutoscaler from a `ScaledObject`, so it needs no metrics store behind it
* Good, because scale-to-zero becomes possible for roles that are idle most of the time, which is what the `edge` profile needs
* Bad, because it is still a component to install, secure, and upgrade in all four profiles, which is the cost [ADR0002](./0002-postgresql-as-index-and-work-queue.md) declined to take on
* Bad, because it claims `external.metrics.k8s.io` cluster-wide, so it conflicts with any adapter already serving that group, though not with a 2c adapter serving only `custom.metrics.k8s.io`, which is a different group
* Neutral, because unlike a broker its absence degrades scaling rather than correctness

### Option 2e: Operator polls queue depth and sets replicas

* Good, because it adds no cluster component and behaves identically in all four profiles, which is true of no other option that tracks the backlog
* Good, because the operator needs no database access, since depth is served over HTTP by a process that already exposes a metrics endpoint (`worker_health.py`), so [ADR0005](./0005-kubernetes-agnostic-api.md) is untouched
* Good, because the scaling policy is expressed in the `Tamoss` resource rather than split across a `ScaledObject` and an adapter rule
* Bad, because it reimplements stabilisation windows and scale-down rate limits that `HorizontalPodAutoscalerBehavior` provides for free
* Bad, because the operator writing `.spec.replicas` fights any HPA a user points at the same Deployment
* Bad, because scaling stops when the operator is down, where an HPA keeps working
