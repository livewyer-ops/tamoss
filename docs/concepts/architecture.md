# Architecture

TAMOSS is an operator-driven Kubernetes product.

The [installation workflow](../operations/install.md) applies
[Helmfile](https://helmfile.readthedocs.io/)-managed
platform releases, then the [Kustomize](https://kustomize.io/)
operator install, then the Kustomize environment overlay containing `Tamoss`
resources.

## Layers

| Layer | Owns | Notes |
| --- | --- | --- |
| Platform | [cert-manager](https://cert-manager.io/), [Traefik](https://traefik.io/), [Authentik](https://goauthentik.io/), [CNPG](https://cloudnative-pg.io/), [RustFS](https://github.com/rustfs/rustfs) Operator, and other shared prerequisites | Installed once per cluster or profile. Third-party components follow their upstream lifecycle. |
| Operator | TAMOSS CRDs, controllers, RBAC, webhooks and reconciliation | Reconciles instances, Storage Backends, Flow Profiles, Ingest Runs and hibernation requests. |
| Instance | TAMS API, worker, Console API, UI, schema migration, generated Secrets, routes and selected backend resources | Declared through a namespaced `Tamoss` CR. |

The operator hides runtime complexity behind Kubernetes custom resources. A
client applies a `Tamoss` CR. The operator renders its workloads, schema
migration, backend integration and status.

For multi-instance clusters, tenant boundaries are Kubernetes namespaces. A
single cluster-wide operator and shared platform install can reconcile multiple
namespaced `Tamoss` instances without introducing a separate tenant API.

## Reconciliation Model

TAMOSS follows Kubernetes convergence patterns:

- `Tamoss` declares one instance.
- `StorageBackend` declares an additional TAMS storage backend for one instance.
- `FlowProfile` registers an immutable TAMS Profile for an instance.
- `IngestRun` records immutable ingest intent, progress and output. The operator
  executes it through a pinned TAMSin Job.
- `TamossHibernate` records a hibernation request and its retained restore data.
- The operator applies canonical resources and corrects drift.
- Status conditions and Events report readiness and user-actionable problems.
- Finalisers clean up operator-owned resources.
- Admission webhooks protect destructive deletes until a confirmation
  annotation is present.

## Runtime Boundaries

The API remains Kubernetes-agnostic. Kubernetes-specific work is done by the
operator:

- The database stores storage backend metadata.
- Referenced Kubernetes Secrets hold credentials.
- The operator derives a runtime credentials Secret per `Tamoss` instance.
- API and worker pods read a mounted credentials file from that Secret.

This allows non-Kubernetes runtimes to provide the same file format without
requiring the API to call Kubernetes APIs.

The Console API reads Kubernetes state for one instance and exposes bounded
operational responses. It checks session roles before accepting commands such
as ingest cancellation. The browser's TAMS API access is read-only; media
creation uses the TAMS API with write authorisation, and managed ingest uses
Kubernetes RBAC. See [Ingest Runs](ingest-runs.md) and
[Flow Profiles](flow-profiles.md) for their ownership and lifecycle rules.
