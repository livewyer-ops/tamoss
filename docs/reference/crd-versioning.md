# CRD Versioning

TAMOSS currently serves `tamoss.livewyer.io/v1alpha1` for both `Tamoss` and
`StorageBackend`. This API is still alpha: field names, enum values, defaults,
and status details may change while the operator moves toward a public beta API.

This page defines the promotion plan. It does not introduce a new CRD version.

## Current Versions

| Kind | Current version | Scope | Status |
| --- | --- | --- | --- |
| `Tamoss` | `v1alpha1` | Namespaced | Alpha, served and stored |
| `StorageBackend` | `v1alpha1` | Namespaced | Alpha, served and stored |

CRD API stability is tracked in this document and release notes until the
project needs a separate served version with conversion.

## Progression Rule

Use `v1alpha2` when material breaking cleanup remains. Use `v1beta1` only when
the desired-state API is stable enough that normal upgrades can preserve user
resources through conversion.

`v1alpha2` is the right next step when any of these are still unresolved:

- provider enum values or provider block shapes still need breaking cleanup
- route exposure needs a breaking choice between Ingress and Gateway API shapes
- secret material in spec needs to move to Secret references
- StorageBackend identity, endpoint, or credential fields need a different
  shape
- conversion behavior cannot preserve existing resources without lossy mapping

Direct `v1beta1` promotion is appropriate only when all of these are true:

- each spec field is classified and either stable or intentionally experimental
- immutable identity fields have API-server validation
- defaulting behavior is documented and tested
- status fields are documented as observational and do not carry secret values
- upgrade tests cover existing `v1alpha1` fixtures
- release notes include migration guidance for deprecated or removed alpha
  fields

`Tamoss` and `StorageBackend` may promote on different timelines. For example,
`StorageBackend` can become beta first if its identity and provider model is
stable while the larger `Tamoss` profile and routing API still needs cleanup.

## Spec Field Classification

The table classifies field groups, not every nested Kubernetes-native field.
Use the CR references for practical field guidance and the CRDs for the
complete schema:

- [Tamoss CR](tamoss-cr.md)
- [StorageBackend CR](storagebackend-cr.md)

### Tamoss Spec

| Field area | Classification | Notes |
| --- | --- | --- |
| `.spec.profile` | Stable candidate | Profile names are core user-facing install shapes: `local-kind`, `single-server`, `multi-server`. Any rename is breaking. |
| `.spec.publicEndpoint` | Stable candidate | Base-domain driven endpoint derivation is the preferred low-boilerplate path. |
| `.spec.backends.db.providedBy` | Stable candidate | `cnpg` and `external` are the supported product modes. |
| `.spec.backends.db.cnpg` | Stable candidate | CNPG is the managed PostgreSQL provider. Backup, monitoring, storage, resources, and instance count are provider-native settings. |
| `.spec.backends.db.external` | Stable candidate | External SQL configuration is required for RDS and similar providers. Secret references should remain the only credential path. |
| `.spec.backends.s3.providedBy` | Stable candidate | `rustfs-operator` and `external` are the supported product modes. |
| `.spec.backends.s3.rustfsOperator` | Stable candidate | RustFS Operator is the managed S3-compatible provider. Pool, bucket, endpoint, and credential settings map to managed resources. |
| `.spec.backends.s3.external` | Stable candidate | External S3-compatible storage uses provider-owned bucket lifecycle and TAMOSS-owned runtime registration. |
| `.spec.auth.providedBy` | Stable candidate | `authentik-blueprints`, `external`, and `none` match managed, external, and disabled authentication modes. |
| `.spec.auth.authentikBlueprints` | Experimental | The managed Authentik shape should settle after more operational use with shared platform Authentik. |
| `.spec.auth.external.oauth2` | Stable candidate | External OAuth/OIDC is a required provider mode. |
| `.spec.auth.trustForwardAuthHeaders` | Experimental | Header trust is security-sensitive and may need tighter naming or validation. |
| `.spec.httpRoute` | Stable candidate | Gateway API routing is the preferred future routing surface. |
| `.spec.ingress` | Likely to change before beta | Keep only if the project commits to first-class Ingress support next to Gateway API. |
| `.spec.service` | Stable candidate | Kubernetes Service exposure remains useful for internal routing. NodePort defaults should stay profile-owned. |
| `.spec.api`, `.spec.ui`, `.spec.worker` | Stable candidate | Workload knobs use Kubernetes-native shapes for images, resources, probes, affinity, tolerations, volumes, env, and replicas. |
| `.spec.networkPolicy` | Stable candidate | Production policy defaults and explicit rule overrides are operationally important. |
| `.spec.serviceAccount` | Stable candidate | Standard Kubernetes service-account settings. |
| `.spec.imagePullSecrets` | Stable candidate | Standard Kubernetes image-pull secret references. |
| `.spec.secrets.apiToken.generate` | Stable candidate | Secret generation is useful for day-zero installs. |
| `.spec.secrets.apiToken.token` | Likely to change before beta | Inline secret values in spec should be reconsidered in favor of Secret references. |
| `.spec.advanced` | Advanced | Operator-facing escape hatches for resource patches and additional resources. Compatibility with provider CRD changes is owned by the user of the advanced field. |
| `.spec.nameOverride` | Likely to change before beta | Naming overrides should be reviewed with `fullnameOverride` before beta. |
| `.spec.fullnameOverride` | Stable candidate, immutable | Controls generated resource names. Already protected as immutable after creation. |
| `.spec.paused` | Stable candidate | Pausing reconciliation is a clear operational control. |

### StorageBackend Spec

| Field area | Classification | Notes |
| --- | --- | --- |
| `.spec.id` | Stable candidate, immutable | BBC TAMS storage backend UUID. Empty derives a deterministic ID. |
| `.spec.tamossRef.name` | Stable candidate, immutable | Same-namespace ownership link to the `Tamoss` instance. |
| `.spec.provider` | Stable candidate, immutable | Current values are `rustfs` and `external-s3`. New providers should add enum values without changing existing semantics. |
| `.spec.defaultStorage` | Stable candidate | Controls default backend registration. |
| `.spec.label`, `.spec.region`, `.spec.storeProduct`, `.spec.storeType` | Stable candidate | These map directly to BBC TAMS storage backend metadata. |
| `.spec.bucketName` | Stable candidate, immutable | Bucket identity is durable and tied to provider lifecycle. |
| `.spec.endpoint` | Stable candidate | Separates internal/default endpoint from browser-facing public endpoint. |
| `.spec.credentials` | Stable candidate | Secret references keep credential values outside the CR. |

## Status Field Classification

Status is observational. Beta compatibility should preserve broad meaning and
condition types where practical, but status may gain fields faster than spec.
Clients should prefer `conditions` and documented fields over brittle JSONPath
assumptions against every nested detail.

### Tamoss Status

| Field area | Classification | Notes |
| --- | --- | --- |
| `.status.observedGeneration` | Stable candidate | Standard Kubernetes status pattern. |
| `.status.conditions` | Stable candidate | Condition types may grow. Existing condition meanings should remain stable after beta. |
| `.status.phase` | Stable candidate | Coarse phase for humans and simple automation. |
| `.status.replicas` | Stable candidate | Observed desired and available replicas by component. |
| `.status.backends`, `.status.auth`, `.status.endpoints` | Stable candidate | Compact summary of selected providers and public endpoints. |
| `.status.providers` | Stable candidate | Provider and ownership summary for `db`, `s3`, `auth`, and `routing`. |
| `.status.resolved` | Experimental | Useful for operations, but generated names and resolved details may expand before beta. |
| `.status.upgrade` | Experimental | Upgrade readiness status should settle after real upgrade workflows. |
| `.status.schemaMigration` | Stable candidate | Migration phase, attempts, and last result are operationally useful. Field additions are acceptable. |
| `.status.schemaVersion` | Likely to change before beta | Review whether this remains top-level or is fully represented by `.status.schemaMigration`. |

### StorageBackend Status

| Field area | Classification | Notes |
| --- | --- | --- |
| `.status.observedGeneration` | Stable candidate | Standard Kubernetes status pattern. |
| `.status.conditions` | Stable candidate | `Ready`, `BucketReady`, and `DatabaseReady` are the primary automation surface. |
| `.status.phase` | Stable candidate | Coarse phase for humans and simple automation. |
| `.status.backendID`, `.status.bucketName` | Stable candidate | Observed durable registration and bucket identity. |
| `.status.resolved` | Stable candidate | Effective provider, endpoints, and credential Secret name. Secret values and key names stay hidden. |

## Fields That Need Conversion Planning

The following areas need explicit conversion decisions before a beta release:

- provider block cleanup if rejected alpha-only fields are removed from a later
  served API version
- `Tamoss.spec.ingress` and `Tamoss.spec.httpRoute` if routing is consolidated
- `Tamoss.spec.secrets.apiToken.token` if inline token values move to a Secret
  reference
- `Tamoss.spec.nameOverride` and `.spec.fullnameOverride` if naming controls are
  consolidated
- `Tamoss.status.schemaVersion` if schema status is consolidated under
  `.status.schemaMigration`
- `StorageBackend.spec.provider` if provider values are renamed or split
- `StorageBackend.spec.endpoint` if endpoint naming changes for non-S3 providers
- `StorageBackend.spec.credentials` if credential references need a provider
  specific union

Status-only shape changes usually do not require data-preserving conversion,
but the beta plan should document changed condition names and replacement fields.

## Served, Storage, and Conversion Strategy

While only `v1alpha1` exists:

- `v1alpha1` remains the only served and storage version.
- Do not add `v1beta1` API packages.
- Do not add new `// +kubebuilder:storageversion` annotations.
- Do not add conversion webhook code.

If `v1alpha2` is needed:

- use it for breaking cleanup while the API is still alpha
- document YAML migration steps in release notes
- keep conversion webhooks optional unless two non-identical versions are served
  at the same time

When `v1beta1` is introduced:

- keep the previous version served for at least one public minor release when
  practical
- make `v1beta1` the storage version only after conversion tests pass
- provide a conversion webhook when served versions have non-identical schemas
- test round trips with real `Tamoss` and `StorageBackend` fixtures
- include downgrade and rollback notes in the release documentation

Conversion tests should cover defaulting, immutable fields, provider unions,
secret references, endpoint derivation, and status preservation expectations.
CEL transition rules must continue to protect immutable fields after conversion.

## Deprecation and Migration Policy

Alpha APIs may still change, but breaking changes require release-note
migration guidance.

After a public beta API exists:

- deprecate fields in docs and CRD comments before removal
- prefer additive fields over semantic changes to existing fields
- keep deprecated beta fields served for at least one public minor release when
  practical
- never expose secret values in status as part of migration support
- document manual migration steps for any field that cannot be converted safely

## Kubernetes API Convention Check

Before promoting a CRD version, review the API against these rules:

- spec is desired state; status is observed state
- destructive behavior is protected by explicit confirmation where applicable
- immutable identity fields are enforced by API-server validation
- provider-specific configuration is modeled as clear discriminated shapes
- references point to Kubernetes resources by name and namespace where needed
- status uses conditions with actionable reasons and messages
- no one-shot action fields are added to spec
- generated resource names and Secret names are observable, but secret values are
  never exposed
