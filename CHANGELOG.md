# Changelog

All notable changes to TAMOSS are documented here.

Release versions track the BBC TAMS API version they implement, followed by an `-ossN` counter for TAMOSS releases against that API version: `8.1.0-oss6` is the sixth TAMOSS release implementing TAMS 8.1. Schema revisions and supported upgrade paths for each release are declared in `operator/compatibility.yaml`.

## 8.2.0-oss3-rc1

### Fixed

- Use SHA256 request checksums for S3 bulk deletion, including OCI Object Storage.
- Send explicit S3 upload checksums in headers so copies retain checksum protection on stores without upload trailer support.
- Resolve dependency security advisories by updating Zod to 4.6.7, Vitest to
  5.0.5, Black to 26.10.1 and virtualenv to 21.14.13.

### Changed

- Update Authentik to 2026.5.7 with chart 2026.5.6 and preserve existing OAuth grants.
- Update the RustFS controller to 0.0.8 and prevent concurrent controller versions during upgrades, retaining the RustFS 1.0.0 runtime.
- Update CloudNativePG to 1.30.1 with chart 0.29.1, retaining PostgreSQL 18.6.

### Upgrade notes

Upgrade directly from `8.2.0-oss2` using the [upgrade guide](docs/operations/upgrades.md).
The TAMOSS schema remains at `8.2.0-oss2`; no new TAMOSS database migration is required.
Apply the platform, then the TAMOSS operator, then select `8.2.0-oss3-rc1` on each instance.

Verify restorable backups and allow a maintenance window for database, identity
and storage restarts. Authentik advances one release series to 2026.5; update
separate outposts to match. Its bundled PostgreSQL stays on major 17.

The RustFS controller applies native workload security defaults and removes
legacy Tenant workload RBAC. Existing Tenants retain their PVCs and credentials.
Custom images and digest references require the
`operator.rustfs.com/runtime-default-image-ack` Tenant annotation set to the
exact image reference before upgrading.
Custom sidecars that use the Kubernetes API require a user-owned ServiceAccount
and explicit token projection; see the
[upstream controller upgrade notes](https://github.com/rustfs/operator/blob/0.0.8/docs/operator-user-guide.md#4-install-the-operator).
Do not downgrade the updated CRDs or workload security state.

## 8.2.0-oss2 - 2026-10-20

### Added

- Select and upgrade each Tamoss instance independently through `spec.version`.
- Configure shared installation defaults while retaining per-instance overrides.
- Support AWS web identity for the default external S3 media backend.
- Support verified TLS connections to external PostgreSQL. See
  [database TLS configuration](docs/reference/runtime-configuration.md#external-database-tls).
- Publish release catalogues and tested dependency versions alongside
  installation assets.

### Fixed

- Correct TAMS 8.2 validation for resource identifiers, metadata and request bodies.
- Preserve Segment timestamp precision and correct paging boundaries.
- Return correct Flow timeranges, including empty ranges for gaps in collections.
- Prevent rejected Segment registrations from changing stored Objects.
- Correct Segment-deletion webhooks and delivery retries.
- Fix playback of ISO segments and media that starts after zero.
- Fix failures caused by oversized response headers and long pagination links.
- Improve database maintenance and storage readiness checks during upgrades.
- Enable TLS routing for Traefik ingress resources.
- Update certifi to 2026.10.19 to remove a trust root affected by compromised
  CA keys ([security advisory](https://github.com/certifi/python-certifi/security/advisories/GHSA-6q48-6c7w-9gw2)).

### Changed

- **Breaking:** organise instance configuration under `instances/<instance>/`.
  Follow the [environment layout migration guide](docs/operations/upgrades.md#environment-layout-migration).
- Update RustFS to 1.0.0 and refresh the tested
  [platform dependencies](deploy/platform/dependencies.yaml).
- Update Python, Go and frontend dependencies, build tools and CI actions.

### Upgrade notes

Upgrade from `8.2.0-oss1` using the [upgrade guide](docs/operations/upgrades.md).
Older installations must follow the supported paths in
[compatibility.yaml](operator/compatibility.yaml).

Before updating the operator, pin each instance to its installed release using
`spec.version`. Review existing component overrides so that intended components
follow the selected release.

This release requires a database migration. Schedule a maintenance window,
stop writes and verify backups before upgrading. Downgrades are unsupported.
See [migration and recovery guidance](docs/operations/upgrades.md#schema-migrations-and-recovery).

Check the integrations you use:

- RustFS: follow the [storage upgrade guidance](docs/operations/upgrades.md#rustfs)
  for credential and presigned URL requirements.
- Authentik: update manually deployed outposts to match the server version.
- Traefik: review custom logging and file-provider settings against the
  [chart 41 changes](https://github.com/traefik/traefik-helm-chart/releases/tag/v41.0.0).
- cert-manager: review the
  [monitoring and permission changes](https://github.com/cert-manager/cert-manager/releases/tag/v1.21.0).
- Additional Nginx gateways: apply the
  [response-header settings](docs/operations/troubleshooting.md#paginated-listings-return-502-through-a-proxy).

Enable optional S3 web identity after upgrading the operator and instance
runtime, following the [configuration guide](docs/operations/s3-web-identity.md).

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
  migration.
- Browser upload controls are replaced by API-client or managed `IngestRun`
  workflows.
- TAMSin `8.2.0-in2` corrects rendered Segment timing. Re-ingest affected media
  as a new run rather than editing or replaying an existing run; stored Flows
  are unchanged.

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
