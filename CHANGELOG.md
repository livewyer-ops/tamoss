# Changelog

All notable changes to TAMOSS are documented here.

This project follows semantic versioning for public releases.

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

## Unreleased

- Preparing the first public TAMOSS release candidate.
- Added a database hibernate/resume lifecycle: `TamossHibernate` exports the
  managed CNPG database to an external S3 hibernation `StorageBackend` with a
  checksummed manifest, and `spec.hibernation.resumeFrom` restores it into a target `Tamoss`
  through CNPG recovery, with configurable artefact retention
  (`Retain`, `DeleteAfterResume`, `TTL`).
- Added the TAMOSS Kubernetes operator as the primary Kubernetes deployment
  path. Local Kubernetes install now creates Kind, applies the operator with
  kustomize, and applies a `Tamoss` custom resource.
- Added zero-to-ready Kind support through `task up`, including bundled
  PostgreSQL and RustFS reconciliation, operator install, CR readiness waiting,
  and operator-focused E2E gates.
- Added a chainsaw-based operator e2e suite with Kind entrypoints, CI reporting,
  and scenario coverage checks.
- Aligned user-facing documentation and task commands to the operator-only
  install model and infrastructure deployment
  profiles.
- Removed application chart deployment assets from this operator-only branch.
- Added S3 backend selection with `providedBy=external|bundled|rustfs-operator`.
  The operator now renders bundled RustFS directly and can coordinate with a
  pinned RustFS Operator install through `rustfs.com/v1alpha1/Tenant`.
- Added PostgreSQL backend selection with `providedBy=external|bundled|cnpg`.
  `providedBy=cnpg` emits a CloudNativePG `Cluster`, waits for CNPG readiness,
  and consumes CNPG-generated app/superuser Secrets.
- Added auth provider selection with `providedBy=external|none|authentik-blueprints`.
  The Authentik path generates stable OAuth2 client credentials, applies a
  per-instance managed Blueprint through the platform Authentik API, and reports
  identity readiness through an OIDC discovery probe.
- The operator CRD now expresses bundled backends with
  `spec.backends.db.providedBy=bundled` and
  `spec.backends.s3.providedBy=bundled`.
- External OAuth2 configuration now lives under `spec.auth.external.oauth2`
  with `spec.auth.providedBy=external`.

## 1.0.0-rc.1

- BBC TAMS v8.0-compatible API surface for sources, flows, flow segments,
  objects, storage allocation, webhooks, and deletion requests.
- PostgreSQL persistence, S3-compatible object storage, asynchronous workers,
  and webhook delivery.
- Operator and Kind automation for local and remote Kubernetes deployment.
- Web UI addon for browsing, operational workflows, preview ingest, and preview
  playback.
