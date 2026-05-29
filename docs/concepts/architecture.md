# Architecture

TAMOSS is an operator-driven Kubernetes product.

The user-facing deployment path is intentionally small:

```bash
task kind:up PROFILE=local-kind
task k8s:init NAME=my-prod PROFILE=multi-server DOMAIN=tamoss.example.com
task k8s:apply ENV=my-prod KUBECONFIG="$KUBECONFIG"
task k8s:wait ENV=my-prod KUBECONFIG="$KUBECONFIG"
```

Those workflows apply three ordered Kustomize layers.

## Layers

| Layer | Owns | Notes |
| --- | --- | --- |
| Platform | cert-manager, Traefik, Authentik, CNPG, RustFS Operator, and other shared prerequisites | Installed once per cluster or profile. Third-party components follow their upstream lifecycle. |
| Operator | TAMOSS CRDs, controller, RBAC, webhooks, and reconciliation logic | Watches `Tamoss` and `StorageBackend` resources. |
| Instance | API, worker, UI, schema migration, generated Secrets, routes, and selected backend resources | Declared through one or more namespaced `Tamoss` CRs. |

The operator hides runtime complexity behind Kubernetes custom resources. A
client applies a `Tamoss` CR; the operator renders the API, worker, UI, schema
migration, backend integration, and status.

For multi-instance clusters, tenant boundaries are Kubernetes namespaces. A
single cluster-wide operator and shared platform install can reconcile multiple
namespaced `Tamoss` instances without introducing a separate tenant API.

## Reconciliation Model

TAMOSS follows Kubernetes convergence patterns:

- `Tamoss` declares one instance.
- `StorageBackend` declares an additional TAMS storage backend for one instance.
- The operator applies canonical resources and corrects drift.
- Status conditions and Events report readiness and user-actionable problems.
- Finalizers clean up operator-owned resources.
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
teaching the API to call Kubernetes APIs.
