# Changelog

All notable changes to TAMOSS are documented here.

Release versions track the BBC TAMS API version they implement, followed by an `-ossN` counter for TAMOSS releases against that API version: `8.1.0-oss6` is the sixth TAMOSS release implementing TAMS 8.1. Schema revisions and supported upgrade paths for each release are declared in `operator/compatibility.yaml`.

## Unreleased

- Preserve large Segment timestamps with exact numeric database bounds. The
  schema upgrade converts existing bounds without changing media or checksums.
- Limit Segment paging timeranges to the returned page, preserving timestamp
  precision and boundary inclusivity.
- Validate Flow replacements before applying Profile metadata. Reject incomplete
  updates while retaining explicit Profile unlinking with a complete definition.

## 8.2.0-oss2-rc2 - 2026-09-23

Release candidate for the second TAMOSS release implementing BBC TAMS 8.2.

- Validate resource identifiers, codec filters and JSON body types consistently.
  Reject exclusive instantaneous timeranges before selection or deletion, and
  keep omitted storage response fields omitted during serialisation.
- Validate HTTP responses against the unmodified BBC schemas and report
  unexercised response branches separately from published OpenAPI alignment.
  Unset bit-rate properties now return 404; see the [API reference](docs/reference/api.md).
- Keep rejected Segment registrations from changing Objects or later entries in
  the same batch. Check that Segment ranges fit within their Objects after
  applying timestamp offsets, including when reusing media.
- Emit `flows/segments_deleted` webhooks during full Flow deletion, retaining
  collection filters until deletion completes. Retry HTTP 401 and 403 responses
  within the existing webhook attempt limit and backoff.
- Play `video/iso.segment` and `audio/iso.segment` media through the
  initialisation-aware HLS preview, rejecting media without an init Object.
  Start playback and replay at the first sample when media begins after zero.
- Publish the tested platform dependency pins with each release. Update to
  Authentik 2026.2.7, CNPG 1.30.0, PostgreSQL 18.6, Traefik 3.7.13 and
  cert-manager 1.21.2 through the normal platform workflow.
- Update RustFS to 1.0.0 while retaining operator 0.0.1. Preserve stored checksums
  when copying media; existing objects need no checksum conversion. See the
  [upgrade guide](docs/operations/upgrades.md) for signing and recovery details.
- Refresh the application dependencies and build toolchains. Platform updates
  stop on failure without attempting to roll back database migrations.

These fixes retain schema revision `8.2.0-oss1`. Upgrade `8.1.0-oss6`
deployments to `8.2.0-oss1` before applying this update.

## 8.2.0-oss2-rc1 - 2026-09-15

Release candidate for the second TAMOSS release implementing BBC TAMS 8.2.

- Fix playback failures caused by oversized Segment response headers.
- Bound collection timerange traversal across cyclic and deeply nested collections.
- Preserve listing filters while keeping labels out of pagination tokens, and
  increase UI proxy response-header buffers for long pagination links.
- Bound HTTP method metric labels and remove unused repository, storage,
  collection and webhook helpers.
- Wait for recorded schema completion in fresh-install checks and compile UI
  assets on the native builder for multi-architecture images.

Upgrades from `8.2.0-oss1` retain schema revision `8.2.0-oss1` and require no
database migration. Deployments with an additional Nginx gateway must also apply
the [pagination response-header settings](https://github.com/livewyer-ops/tamoss/blob/8.2.0-oss2-rc1/docs/operations/troubleshooting.md#paginated-listings-return-502-through-a-proxy).

## 8.2.0-oss1 - 2026-09-09

TAMOSS 8.2.0-oss1 is the first stable TAMOSS release implementing BBC TAMS 8.2.
It brings richer media lifecycle metadata, reusable Flow Profiles and
declarative, Kubernetes-managed ingest.

### Added

- BBC TAMS 8.2 Profiles, first-class Flow lifecycle status, initialisation
  Objects, collection filters and deterministic listings.
- Declarative `IngestRun` resources: durable requests, progress, cancellation,
  retry lineage and output identities.
- Managed TAMSin `8.2.0-in2` ingest: controlled HTTPS/S3 sources and
  streaming/staged handling.
- Kubernetes `FlowProfile` management integrated with ingest.
- Console views for Profiles, ingest history, runtime information and media
  playback.

### Improved

- Joint audio/video buffering and supported multi-Object playback.
- Portrait sizing, play/pause controls, stalled-load handling and player cleanup.
- Storage concurrency, deletion guards, hibernation retries and repeated restore
  handling.
- Dependency updates and signed multi-architecture release packaging.

### Upgrade Notes

- The declared direct upgrade starts at `8.1.0-oss6` and requires a database
  migration. Existing 8.2 candidates retain the same schema revision.
- Browser upload controls are replaced by API-client or managed `IngestRun`
  workflows.

## 8.1.0-oss6 - 2026-08-08

- Added worker health and readiness endpoints, and moved worker workloads onto
  HTTP probes.
- Added targeting of individual instances within an environment.
- Made the Authentik issuer probe timeout configurable, separated retrying
  blueprint failures from terminal ones, and relaxed the platform Authentik
  worker probe timeouts.
- Reserved the managed operator metrics endpoints.
- Replaced the UI logo PNG with a scalable vector asset.

## 8.1.0-oss5 - 2026-08-06

- Added managed OAuth support and install hardening to the `edge` profile.
- Added an optional node memory budget assertion to the deployed checks.
- Defaulted the `single-server` profile RustFS disk check for single-disk hosts.
- Made the mainline install golden path self-consistent.
- Resolved the react-router and cryptography security advisories.
- Unified the documentation and closed the install-test gaps.

## 8.1.0-oss4 - 2026-07-22

- Added a declarative hibernate and resume lifecycle: `TamossHibernate` exports
  the managed CNPG database to an external S3 hibernation `StorageBackend` with
  a checksummed manifest, and `spec.hibernation.resumeFrom` restores it into a
  target `Tamoss` through CNPG recovery, with configurable artifact retention
  (`Retain`, `DeleteAfterResume`, `TTL`).
- Added the `edge` profile for single-node ARM installs.
- Propagated browser CORS origins to managed S3, and preflighted the bucket URL
  in the external S3 CORS diagnostic.
- Avoided redundant Authentik blueprint applies.
- Improved the environment summary output.
- Clarified aqua installation, optional media tooling, and browser CORS
  configuration in the documentation.

## 8.1.0-oss3 - 2026-06-29

- Added configurable API CORS origins.
- Exposed internal TAMOSS API metrics.
- Matched scoped auth routes with FastAPI route contexts.
- Allowed CORS preflight for HEAD requests.
- Made Authentik proxy application reconciliation idempotent.

## 8.1.0-oss2 - 2026-06-11

- Aligned TAMS 8.1 edge-case behaviours for unset properties, empty webhook
  event lists, invalid flow identifiers and non-JSON property writes, with
  matching conformance coverage.
- Improved segment ingest, webhook delivery and worker queue throughput with
  new database indexes (schema revision 8.1.0-oss2).
- Upgraded the Python runtime to 3.14 and the operator toolchain to Go 1.26,
  controller-runtime 0.24 and Kubernetes 1.36.
- Reworked operator reconciliation around server-side apply, garbage-collected
  cleanup, single-pass status and indexed Secret watches.
- Extended operator e2e gates for drift correction, field ownership and secret
  rotation; refactored API routes and persistence helpers; improved the
  operator development inner loop.

## 8.1.0-oss1 - 2026-06-03

- Updated TAMS contract support to 8.1.
- Restructured the TAMS conformance tests.

## 8.0.0-oss3 - 2026-06-02

- Fixed release schema metadata builds.

## 8.0.0-oss2 - 2026-06-02

- Added OAuth2 route scope authorization.
- Enforced the presigned timeout contract.
- Hardened storage allocation first use.
- Split the operator release and schema versions.
- Bumped PyJWT for the security audit.

## 8.0.0-oss1 - 2026-06-02

First release on the TAMS-aligned version scheme.

- Replaced the Helm deployment path with operator-managed Kustomize, making the
  TAMOSS Kubernetes operator the primary Kubernetes deployment path. Local
  Kubernetes install creates Kind, applies the operator with kustomize, and
  applies a `Tamoss` custom resource.
- Added S3 backend selection with `providedBy=external|bundled|rustfs-operator`.
  The operator renders bundled RustFS directly and can coordinate with a pinned
  RustFS Operator install through `rustfs.com/v1alpha1/Tenant`.
- Added PostgreSQL backend selection with `providedBy=external|bundled|cnpg`.
  `providedBy=cnpg` emits a CloudNativePG `Cluster`, waits for CNPG readiness,
  and consumes CNPG-generated app/superuser Secrets.
- Added auth provider selection with
  `providedBy=external|none|authentik-blueprints`. The Authentik path generates
  stable OAuth2 client credentials, applies a per-instance managed Blueprint
  through the platform Authentik API, and reports identity readiness through an
  OIDC discovery probe.
- Expressed bundled backends through `spec.backends.db.providedBy=bundled` and
  `spec.backends.s3.providedBy=bundled`, and moved external OAuth2 configuration
  under `spec.auth.external.oauth2` with `spec.auth.providedBy=external`.
- Added zero-to-ready Kind support, published operator install manifests, and
  added a chainsaw-based operator e2e suite with Kind entrypoints, CI reporting,
  and scenario coverage checks.
- Aligned user-facing documentation and task commands to the operator-only
  install model and the infrastructure deployment profiles.
- Removed the application chart deployment assets.

## 1.0.0-rc.1

Predates the TAMS-aligned version scheme introduced in `8.0.0-oss1`.

- BBC TAMS v8.0-compatible API surface for sources, flows, flow segments,
  objects, storage allocation, webhooks, and deletion requests.
- PostgreSQL persistence, S3-compatible object storage, asynchronous workers,
  and webhook delivery.
- Operator and Kind automation for local and remote Kubernetes deployment.
- Web UI addon for browsing, operational workflows, preview ingest, and preview
  playback.
