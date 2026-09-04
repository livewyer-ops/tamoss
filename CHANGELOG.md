# Changelog

All notable changes to TAMOSS are documented here.

Release versions track the BBC TAMS API version they implement, followed by an `-ossN` counter for TAMOSS releases against that API version: `8.1.0-oss6` is the sixth TAMOSS release implementing TAMS 8.1. Schema revisions and supported upgrade paths for each release are declared in `operator/compatibility.yaml`.

## Unreleased

- Preparing the first public TAMOSS release candidate with BBC TAMS 8.2
  compatibility.
- Made UI requests to a disabled Console API fail explicitly with a JSON 503
  response instead of falling through to the single-page application shell.
- Added complete playback for independently decodable MP4 Object sequences,
  including split video and audio Flows.
- Realigned the vendored BBC TAMS contract with the upstream 8.2 bugfix retag,
  restoring the `codec`, `container`, `avg_bit_rate`, `segment_duration` and
  `container_mapping` properties on multi-essence Flows, with conformance
  coverage that validates them. The advertised API version stays 8.2, matching
  upstream's versioning policy for this fix.
- Applied the official TAMS Brand Kit to the service page and navigation.
- Fixed `task kind:up` leaving application workloads on the previous build:
  operand images are now tagged by `src/` content, so a rebuild changes the
  rendered Deployment spec and Kubernetes rolls the api, ui and worker pods
  without an imperative restart.

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
