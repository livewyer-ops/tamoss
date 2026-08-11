# TAMOSS Documentation

TAMOSS is an operator-led Kubernetes product. Start from the outcome you need,
then stay on the matching install, configuration, usage, and operations path.

## Start Here

| Goal | Start with | Next steps |
| --- | --- | --- |
| Validate TAMOSS locally | [Local Kind](getting-started/local-kind.md) | [Usage](usage.md), [Troubleshooting](operations/troubleshooting.md), [Task Commands](reference/task-commands.md) |
| Install on an existing cluster | [Install](operations/install.md) | [Edge](getting-started/edge.md), [Single Server](getting-started/single-server.md), or [Multi Server](getting-started/multi-server.md), then [Configuration](configuration.md) |
| Choose a deployment shape | [Deployment Profiles](concepts/profiles.md) | `local-kind`, `edge`, `single-server`, or `multi-server` |
| Configure providers and endpoints | [Configuration](configuration.md) | [Provider Ownership](concepts/provider-ownership.md), [Runtime Configuration](reference/runtime-configuration.md), [Tamoss CR](reference/tamoss-cr.md), [StorageBackend CR](reference/storagebackend-cr.md) |
| Use the UI or API | [Usage](usage.md) | [API](reference/api.md), [Storage Backends](concepts/storage-backends.md) |
| Monitor or cancel ingest work | [Manage Ingest Runs](operations/manage-ingest-runs.md) | [Ingest Runs](concepts/ingest-runs.md), [IngestRun CR](reference/ingestrun-cr.md) |
| Operate an installed cluster | [Day 2](operations/day-2.md) | [Backup and Restore](operations/backup-restore.md), [Hibernate and Resume](operations/hibernate-resume.md), [Upgrades](operations/upgrades.md), [Troubleshooting](operations/troubleshooting.md) |
| Develop or test TAMOSS | [Development Workflow](development/contributing.md) | [Testing](development/testing.md), [Task Commands](reference/task-commands.md) |

## Get Started

- [Local Kind](getting-started/local-kind.md) - canonical local validation path.
- [Edge](getting-started/edge.md) - single-node ARM64 self-contained
  Kubernetes.
- [Single Server](getting-started/single-server.md) - run on one node or a
  small self-managed Kubernetes cluster.
- [Multi Server](getting-started/multi-server.md) - production-shaped
  self-managed Kubernetes.
- [Configuration](configuration.md) - durable product and provider
  configuration.
- [Usage](usage.md) - web UI, API, ingest, presigned URLs, and deletion.

## Concepts

- [Architecture](concepts/architecture.md) - operator, platform, and instance
  boundaries.
- [Deployment Profiles](concepts/profiles.md) - `local-kind`, `edge`, `single-server`,
  and `multi-server`.
- [Flow Profiles](concepts/flow-profiles.md) - immutable technical metadata
  expanded into self-contained Flows.
- [Initialisation Objects](concepts/initialisation-objects.md) - shared setup
  data for fragmented media.
- [Provider Ownership](concepts/provider-ownership.md) - managed and external
  PostgreSQL, S3, authentication, and HTTP.
- [Storage Backends](concepts/storage-backends.md) - default and additional
  TAMS storage backends.
- [Ingest Runs](concepts/ingest-runs.md) - durable ingest intent, execution,
  and history.
- [Tenancy](concepts/tenancy.md) - namespace-based tenant boundaries and shared
  platform consumption.

## Operations

- [Install](operations/install.md) - apply platform, operator, and instance
  layers.
- [Day 2](operations/day-2.md) - status, scaling, pausing, drift, and logs.
- [Backup and Restore](operations/backup-restore.md) - data ownership and
  restore planning.
- [Hibernate and Resume](operations/hibernate-resume.md) - on-demand managed
  database hibernation artefacts and recovery.
- [Manage Ingest Runs](operations/manage-ingest-runs.md) - inspect history,
  follow workloads, and cancel active runs.
- [Upgrades](operations/upgrades.md) - change sequencing and rollback.
- [Deletion Protection](operations/deletion-protection.md) - required
  confirmation annotations and finalizers.
- [Troubleshooting](operations/troubleshooting.md) - diagnose readiness and
  runtime failures.

## Reference

- [Tamoss CR](reference/tamoss-cr.md) - instance resource reference.
- [StorageBackend CR](reference/storagebackend-cr.md) - additional object-store
  backend reference.
- [IngestRun CR](reference/ingestrun-cr.md) - ingest request, lifecycle, and
  status fields.
- [CRD Versioning](reference/crd-versioning.md) - API compatibility,
  conversion, and deprecation plan.
- [Runtime Configuration](reference/runtime-configuration.md) - workload,
  image, and runtime environment overrides.
- [Task Commands](reference/task-commands.md) - supported operational command
  surface.
- [API](reference/api.md) - BBC TAMS API and product health endpoints.

## Development

- [Contributing](../CONTRIBUTING.md) - public contribution workflow.
- [Development Workflow](development/contributing.md) - local product
  development.
- [Testing](development/testing.md) - local, operator, and deployed gates.
- [Documentation Structure](development/documentation.md) - Diataxis page
  types and authoring rules.
- [UI Overhaul Design Records](development/ui-overhaul/README.md) - internal
  architecture decisions and unresolved release gates.

Docs are maintained as raw Markdown. Revisit a static site generator only if the
documentation set grows beyond roughly 50 maintained pages or Markdown
navigation becomes a clear maintenance burden.

## Glossary

| Term | Meaning |
| --- | --- |
| `Tamoss` | The namespaced custom resource reconciled into API, UI, worker, database, storage, identity, and routing resources. |
| `StorageBackend` | A namespaced custom resource that represents a registered TAMS object-store backend and its readiness. |
| `IngestRun` | A durable, namespaced request and history record for one approved TAMSin ingest attempt. |
| TAMSin Job | The temporary Kubernetes `Job` created and owned by the operator for an `IngestRun`; it is not a user-authored ingest API. See [TAMSin](https://github.com/livewyer-ops/tamsin). |
| Deployment profile | A Kubernetes deployment shape such as `local-kind`, `edge`, `single-server`, or `multi-server` that supplies defaults. |
| Flow Profile | An immutable TAMS technical-metadata definition eagerly expanded into each linked Flow. |
| Environment | A checked-in Kustomize overlay under `deploy/environments/<name>` used for durable cluster configuration. |
| Managed | TAMOSS reconciles the Kubernetes-side resource lifecycle after prerequisites exist. |
| External | TAMOSS consumes references to a service owned outside TAMOSS and does not mutate that service. |
