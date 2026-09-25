# Tamoss CR Reference

`Tamoss` declares one TAMOSS instance in a namespace. Its release selects images
and the schema target. Omitted site settings inherit installation and profile
defaults; explicit fields override those defaults.
The canonical CRD in `operator/config/crd/bases/` remains the exhaustive schema
source.

Group: `tamoss.livewyer.io`

Version: `v1alpha1`

Kind: `Tamoss`

Scope: `Namespaced`

<a id="minimal-shape"></a>

## Minimal resource

Configure [installation defaults](../configuration.md#installation-defaults), then
select the instance release:

```yaml
apiVersion: tamoss.livewyer.io/v1alpha1
kind: Tamoss
metadata:
  name: media
  namespace: team
spec:
  version: <release>
```

`<release>` is an exact release supported by the installed operator. Without
installation defaults, set `spec.profile` and the required site settings explicitly.
`spec.publicEndpoint.baseDomain` overrides the derived instance domain.
Inherited settings are resolved without writing them into the resource;
Kubernetes can still populate fields with defaults declared in the CRD.

See the [minimal local-kind example](../../deploy/environments/local-kind/tamoss-minimal.example.yaml)
and the [annotated single-server example](../../deploy/environments/single-server/tamoss-full.example.yaml)
for instance manifests. The commented settings in the single-server example
inherit their defaults until explicitly enabled.

Set `publicEndpoint.uiURL` when the public UI uses a non-standard external
port. It is an exact origin, not a path:

```yaml
spec:
  publicEndpoint:
    baseDomain: tamoss.example.com
    uiURL: https://app.tamoss.example.com:30443
```

## Common Spec Areas

| Field | Purpose |
| --- | --- |
| `.spec.version` | Exact product release from the operator installation catalogue. Omission reports `VersionRequired` and leaves workloads unchanged. |
| `.spec.profile` | Overrides the installation profile with `local-kind`, `edge`, `single-server`, or `multi-server`. |
| `.spec.publicEndpoint` | Overrides instance API, UI and S3 endpoints. Managed Authentik uses the shared installation URL when configured. |
| `.spec.backends.db` | Selects managed [CNPG](https://cloudnative-pg.io/) or external PostgreSQL and configures database backup/restore when CNPG is used. |
| `.spec.backends.s3` | Selects managed [RustFS](https://github.com/rustfs/rustfs) Operator or external S3-compatible storage for the default backend. |
| `.spec.backends.s3.tags` | Freeform metadata with string or string-array values, advertised on the operator-managed default TAMS storage backend. |
| `.spec.auth` | Selects Authentik Blueprints, external OAuth/OIDC, or no authentication. |
| `.spec.api`, `.spec.ui`, `.spec.worker`, `.spec.console` | Component enablement, replicas, images, resources, scheduling, probes, env, volumes, and security context. |
| `.spec.service`, `.spec.ingress`, `.spec.httpRoute` | Service and public routing configuration. |
| `.spec.networkPolicy` | Profile default NetworkPolicy settings, component overrides, and destination-scoped Kubernetes API IP blocks for Console. |
| `.spec.secrets.apiToken` | Generated or explicit API token configuration. |
| `.spec.images` | Shared helper images that are not owned by one component. |
| `.spec.ingest.sourcePolicy.mode` | Ingest source mode: `Disabled`, `PublicHTTPS`, or `Restricted`. Production profiles default to `Disabled`; `local-kind` defaults to `PublicHTTPS`. |
| `.spec.ingest.sources` | Reusable named HTTP and S3 source boundaries, including optional source-owned credential Secret references. See [IngestRun CR](ingestrun-cr.md). |
| `.spec.hibernation` | Captures managed CNPG state to an archive StorageBackend and removes database compute; disabling it resumes the instance. See [Hibernate and Resume](../operations/hibernate-resume.md). |
| `.spec.advanced` | Advanced resource patches and additional resources for provider fields that do not have first-class TAMOSS fields. |
| `.spec.paused` | Stops reconcile writes while still allowing status updates. |

Use [Configuration](../configuration.md) for practical examples and
[Provider Ownership](../concepts/provider-ownership.md) for managed and
external provider responsibilities.

Webhook private-egress allowlists are runtime environment settings. Configure
the same `TAMOSS_WEBHOOK_ALLOWED_HOSTS` and
`TAMOSS_WEBHOOK_ALLOW_PRIVATE_TARGETS` values in both `.spec.api.env` and
`.spec.worker.env` so API validation and worker delivery enforce one policy.

Use `.spec.advanced` only when a field is not represented elsewhere in the CR.
Advanced resource patches are applied before the operator writes emitted
resources, and advanced extra resources are owned by the `Tamoss` instance.

## API CORS

Use `.spec.api.cors.allowedOrigins` and, where regex matching is needed,
`.spec.api.cors.allowedOriginRegexes` when a browser application hosted on a
different origin needs to call the TAMOSS API directly or access
operator-managed RustFS through the browser-facing S3 ingress:

```yaml
spec:
  api:
    cors:
      allowedOrigins:
        - https://app.tamoss.example.com
        - https://tool.example.com
      allowedOriginRegexes:
        - ^https://[a-z0-9-]+\.example-pages\.com$
```

The operator renders exact origins into `TAMOSS_CORS_ALLOWED_ORIGINS` and
regexes into `TAMOSS_CORS_ALLOWED_ORIGIN_REGEXES` for the API Deployment. The
API then allows browser preflight and authenticated requests from matching
origins. Exact origin values must be absolute `http` or `https` origins without
a path, query string, or fragment.

For managed RustFS backends, the operator also applies
`spec.publicEndpoint.uiURL` and exact allowed origins to bucket CORS and to
[Traefik](https://traefik.io/) S3 ingress middleware. This preserves a
non-standard external UI port for browser media requests.
`allowedOriginRegexes` is
also applied to Traefik S3 ingress middleware, but not to S3 bucket CORS. For
`external-s3` backends, update the bucket CORS policy separately for every
browser origin that dereferences presigned `put_url` or `get_urls`; see
[Storage Backends](../concepts/storage-backends.md).

## Immutability and Required Fields

The API server rejects updates that change `.spec.fullnameOverride` after
creation because it controls generated Kubernetes resource names. Updates that
keep the same non-empty value are accepted.

When `.spec.backends.s3.providedBy: external` or the external S3 block is set,
`.spec.backends.s3.external.endpoint.default.url` is required.

## Status Summary

| Field | Purpose |
| --- | --- |
| `.status.currentVersion` | Last release whose schema migration and workload rollouts completed. |
| `.status.appliedDefaultsRevision` | Installation defaults revision whose reconciliation and rollouts completed. |
| `.status.resolved.defaults` | Loaded defaults file source and SHA-256 revision. |
| `.status.resolved.profile` | Effective deployment profile. |
| `.status.auth.platformNamespace` | Effective shared Authentik namespace. |
| `.status.conditions` | Readiness, backend, identity, routing, schema, upgrade, and degraded conditions. |
| `.status.endpoints` | Effective API, UI, S3 and shared authentication URLs. |
| `.status.providers` | Selected provider and ownership model for database, S3, authentication, and routing. |
| `.status.resolved.images` | Effective API, UI, worker, Console, TAMSin, schema helper, CNPG Postgres, and RustFS image references where rendered. |
| `.status.resolved.versions` | Selected TAMOSS release and schema target, and BBC TAMS API compatibility version. |
| `.status.resolved.generatedSecrets` | Generated Secret names only. Secret values are never exposed. |
| `.status.resolved.resources` | Generated workload and default StorageBackend resource names. |
| `.status.resolved.routes` | Generated route object names for API and UI exposure. |
| `.status.backupPolicy` | Managed CNPG backup policy state, CNPG resource names, and observed backup timestamps. |
| `.status.schemaMigration` | Migration phase, attempts, applied revision, and final result. |
| `.status.upgrade` | Upgrade readiness summary. |
| `.status.lifecycle` | Hibernate/resume lifecycle phase (`Running`, `Hibernating`, `Hibernated`, `Resuming`, `Failed`), the active operation reference, and the last hibernate and resume references. `.status.phase` also reports `Hibernating`, `Hibernated`, and `Resuming` while the lifecycle gate is active. See [Hibernate and Resume](../operations/hibernate-resume.md). |
| `.status.lifecycle.pendingArtifactCleanups` | Archive retention work from earlier restore cycles, including expiry timestamps and unresolved cleanup failures. |

Use [CRD Versioning](crd-versioning.md) for API stability and migration policy.
