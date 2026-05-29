# Provider Ownership

TAMOSS separates platform dependencies into managed and external ownership
models. The choice is independent for PostgreSQL, S3-compatible storage,
authentication, and HTTP ingress.

| Area | Managed option | External option | TAMOSS operator responsibility |
| --- | --- | --- | --- |
| PostgreSQL | `backends.db.providedBy: cnpg` | `backends.db.providedBy: external` | Create and monitor CNPG instance resources, or consume external host/database/Secret references. |
| S3 | `backends.s3.providedBy: rustfs-operator` and `StorageBackend.provider: rustfs` | `backends.s3.providedBy: external` and `StorageBackend.provider: external-s3` | Create RustFS instance and bucket resources for managed mode, or register external endpoint/bucket/Secret metadata. |
| Authentication | `auth.providedBy: authentik-blueprints` | `auth.providedBy: external` | Reconcile Authentik Application/OAuth provider through managed Blueprints, or consume external OIDC metadata and client Secret references. |
| HTTP | Reference Traefik platform with Kubernetes `Ingress` or `HTTPRoute` | External ingress or Gateway implementation | Render standard routing objects from the `Tamoss` CR; do not install or mutate external controllers. |

## Managed Means

Managed mode means the TAMOSS operator owns instance resources after the
required platform capability exists. For example, with `providedBy: cnpg`, the
operator creates a CNPG `Cluster` resource, waits for readiness, consumes
generated Secrets, and runs schema migration.

Managed does not mean the `Tamoss` reconcile installs third-party platform
operators. Cluster administrators or the checked-in platform bootstrap path
install CNPG, RustFS Operator, Authentik, cert-manager, Traefik, Gateway API
CRDs, and Gateway API providers.

Managed integrations are not all CR-driven:

| Integration | TAMOSS mechanism |
| --- | --- |
| CNPG | Emits `Cluster` and optional `ScheduledBackup` CRs after the CNPG CRDs exist. |
| RustFS | Emits a RustFS `Tenant` CR, then manages the bucket through the S3 API. |
| Gateway API | Emits `HTTPRoute` resources only; Gateways and GatewayClasses stay platform-owned. |
| Traefik | Emits `Middleware` resources only when Traefik-specific ingress integration is selected. |
| Authentik | Uses the Authentik HTTP API for managed Blueprints and proxy outpost configuration; no Authentik CRs are emitted. |
| cert-manager | No cert-manager CRs are emitted. TLS issuer/certificate lifecycle remains platform-owned or ingress-shim-owned. |

`Tamoss.status.providers` reports the selected provider and ownership model for
database, S3, authentication, and routing. Use it with conditions to see whether
TAMOSS owns lifecycle for an instance resource or is consuming an external
service.

Common provider fields:

```yaml
spec:
  backends:
    db:
      providedBy: cnpg
    s3:
      providedBy: rustfs-operator
  auth:
    providedBy: authentik-blueprints
```

Use `providedBy: external` when PostgreSQL, S3, authentication, or HTTP ingress
is owned outside TAMOSS. In external mode, provide endpoint and Secret
references in the CR and manage provider lifecycle outside TAMOSS.

## Advanced Resource Customization

First-class `Tamoss` fields are the preferred configuration path for common
operational controls. For provider fields that change faster than the TAMOSS
API should, `spec.advanced` provides explicit operator-facing escape hatches:

- `spec.advanced.resourcePatches` applies JSON merge patches to matching
  TAMOSS-emitted resources before server-side apply.
- `spec.advanced.extraResources` adds extra Kubernetes resources owned by the
  `Tamoss` instance.

Advanced patches select emitted resources by generated `kind` and `name`, with
optional `apiVersion` disambiguation. Patches may set labels, annotations, and
spec fields, but not resource identity or controller ownership fields such as
`kind`, `apiVersion`, `metadata.name`, `metadata.namespace`, or
`metadata.ownerReferences`.

The operator keeps first-class fields stable where possible. Advanced provider
YAML remains the responsibility of the operator using it: if a third-party CRD
changes field names or semantics, update the advanced YAML alongside that
provider upgrade.

## External Means

External mode means TAMOSS consumes configuration. The external provider remains
owned outside TAMOSS.

The operator does not create, mutate, upgrade, back up, restore, rotate, or
delete external:

- PostgreSQL databases or users.
- S3 buckets, IAM policies, lifecycle rules, retention, replication, CORS, or
  encryption.
- OAuth/OIDC clients, realms, signing keys, groups, or scopes.
- Ingress controllers, load balancers, DNS zones, TLS issuers, or certificates.

External resources must be operationally ready before the `Tamoss` or
`StorageBackend` resource references them.

In multi-tenant clusters, provider ownership is still selected per namespaced
`Tamoss` instance. Shared platform services such as Authentik, Traefik, Gateway
API providers, CNPG Operator, and RustFS Operator remain outside tenant
ownership.
