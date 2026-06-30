# Tamoss CR Reference

`Tamoss` declares one TAMOSS instance in a namespace. The operator applies
profile defaults first, then explicit fields in the CR override those defaults.
The canonical CRD in `operator/config/crd/bases/` remains the exhaustive schema
source.

Group: `tamoss.livewyer.io`

Version: `v1alpha1`

Kind: `Tamoss`

Scope: `Namespaced`

## Minimal Shape

```yaml
apiVersion: tamoss.livewyer.io/v1alpha1
kind: Tamoss
metadata:
  name: tamoss-kind
  namespace: tams
spec:
  profile: local-kind
```

For non-local profiles, provide a public base domain unless every public
endpoint is configured directly:

```yaml
spec:
  profile: multi-server
  publicEndpoint:
    baseDomain: tamoss.example.com
```

## Common Spec Areas

| Field | Purpose |
| --- | --- |
| `.spec.profile` | Selects `local-kind`, `single-server`, or `multi-server` defaults. |
| `.spec.publicEndpoint` | Derives public API, UI, S3, and Authentik endpoint defaults. |
| `.spec.backends.db` | Selects managed CNPG or external PostgreSQL and configures database backup/restore when CNPG is used. |
| `.spec.backends.s3` | Selects managed RustFS Operator or external S3-compatible storage for the default backend. |
| `.spec.auth` | Selects Authentik Blueprints, external OAuth/OIDC, or no authentication. |
| `.spec.api`, `.spec.ui`, `.spec.worker` | Component enablement, replicas, images, resources, scheduling, probes, env, volumes, and security context. |
| `.spec.service`, `.spec.ingress`, `.spec.httpRoute` | Service and public routing configuration. |
| `.spec.networkPolicy` | Profile default NetworkPolicy settings and overrides. |
| `.spec.secrets.apiToken` | Generated or explicit API token configuration. |
| `.spec.images` | Shared helper images that are not owned by one component. |
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

Use `.spec.api.cors.allowedOrigins` when a browser application hosted on a
different origin needs to call the TAMOSS API directly:

```yaml
spec:
  api:
    cors:
      allowedOrigins:
        - https://app.tamoss.example.com
        - https://tool.example.com
```

The operator renders these origins into `TAMOSS_CORS_ALLOWED_ORIGINS` for the
API Deployment. The API then allows browser preflight and authenticated requests
from those origins. Values must be absolute `http` or `https` origins without a
path, query string, or fragment.

This field only configures CORS on TAMOSS API responses. Browser uploads,
playback, and downloads that use presigned object-store URLs are controlled by
the object-store provider's CORS policy. For `external-s3` backends, update the
bucket CORS policy separately for every browser origin that dereferences
presigned `put_url` or `get_urls`.

## Immutability And Required Fields

The API server rejects updates that change `.spec.fullnameOverride` after
creation because it controls generated Kubernetes resource names. Updates that
keep the same non-empty value are accepted.

When `.spec.backends.s3.providedBy: external` or the external S3 block is set,
`.spec.backends.s3.external.endpoint.default.url` is required.

## Status Summary

| Field | Purpose |
| --- | --- |
| `.status.conditions` | Readiness, backend, identity, routing, schema, upgrade, and degraded conditions. |
| `.status.endpoints` | Effective API and UI URLs after profile defaults and endpoint overrides. |
| `.status.providers` | Selected provider and ownership model for database, S3, authentication, and routing. |
| `.status.resolved.images` | Effective API, UI, worker, schema helper, CNPG Postgres, and RustFS image references where rendered. |
| `.status.resolved.versions` | Effective TAMOSS schema, runtime, and BBC TAMS API compatibility versions. |
| `.status.resolved.generatedSecrets` | Generated Secret names only. Secret values are never exposed. |
| `.status.resolved.resources` | Generated workload and default StorageBackend resource names. |
| `.status.resolved.routes` | Generated route object names for API and UI exposure. |
| `.status.backupPolicy` | Managed CNPG backup policy state, CNPG resource names, and observed backup timestamps. |
| `.status.schemaMigration` | Migration phase, attempts, applied revision, and final result. |
| `.status.upgrade` | Upgrade readiness summary. |

Use [CRD Versioning](crd-versioning.md) for API stability and migration policy.
