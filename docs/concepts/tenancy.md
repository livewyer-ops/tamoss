# Tenancy

TAMOSS tenancy is namespace-based. A tenant is a Kubernetes namespace containing
one or more `Tamoss` resources, their `StorageBackend` resources, referenced
Secrets, and the workloads reconciled by the operator.

TAMOSS does not provide a `Tenant`, `TamossTenant`, or `TamossPlatform` CRD.
Use Kubernetes namespaces and standard policy resources for tenant boundaries.

## Namespace Contract

Create a namespace before applying tenant-owned resources. Recommended metadata:

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: tams-team-a
  labels:
    app.kubernetes.io/part-of: tamoss
    tamoss.livewyer.io/environment: production
    tamoss.livewyer.io/tenant: team-a
  annotations:
    tamoss.livewyer.io/owner: platform-team
```

Apply tenant-local controls before the `Tamoss` CR where your cluster enforces
them:

```yaml
apiVersion: v1
kind: ResourceQuota
metadata:
  name: tamoss-quota
  namespace: tams-team-a
spec:
  hard:
    requests.cpu: "8"
    requests.memory: 16Gi
    requests.storage: 500Gi
---
apiVersion: v1
kind: LimitRange
metadata:
  name: tamoss-defaults
  namespace: tams-team-a
spec:
  limits:
    - type: Container
      defaultRequest:
        cpu: 100m
        memory: 128Mi
      default:
        cpu: "1"
        memory: 512Mi
```

Tenant operators should also apply namespace RBAC for humans and automation that
can edit `Tamoss`, `StorageBackend`, and referenced Secrets. Keep platform
component administration outside tenant namespaces.

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: tamoss-tenant-editor
  namespace: tams-team-a
rules:
  - apiGroups: ["tamoss.livewyer.io"]
    resources: ["tamosses", "storagebackends"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
  - apiGroups: [""]
    resources: ["secrets"]
    verbs: ["get", "list", "watch", "create", "update", "patch"]
```

## Shared Platform Services

One shared platform install can serve many tenant namespaces. Tenants consume
platform capabilities; they do not own platform lifecycle.

| Area | Tenant reference | Platform owner |
| --- | --- | --- |
| Authentication | `spec.auth.authentikBlueprints.platformNamespace` and token Secret reference | Authentik namespace and Authentik service lifecycle |
| Routing | `Ingress` or `HTTPRoute` fields in each `Tamoss` CR | Traefik, GatewayClass, Gateway, DNS, and certificates |
| PostgreSQL | CNPG `Cluster` resources created in the tenant namespace when managed | CNPG operator install and upgrades |
| S3 | RustFS `Tenant` and `StorageBackend` resources in the tenant namespace when managed | RustFS Operator install and upgrades |

External providers follow the same namespace boundary for configuration:
tenant-local Secrets and CRs reference services that are owned outside TAMOSS.

## Network Boundaries

Use NetworkPolicy where your CNI enforces it. A tenant namespace usually needs:

- DNS egress.
- Egress to Kubernetes API only if tenant workloads require it.
- Egress to Authentik when managed authentication is enabled.
- Egress to external PostgreSQL or S3 endpoints when those providers are
  external.
- Ingress from the cluster ingress or Gateway implementation to TAMOSS services.
- Prometheus scrape ingress when monitoring is enabled.

Managed CNPG and RustFS resources may need additional namespace-local traffic
between their pods and services.

Start from a default-deny policy only when the corresponding allow policies are
ready:

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: default-deny
  namespace: tams-team-a
spec:
  podSelector: {}
  policyTypes:
    - Ingress
    - Egress
```

## Watch Scope

The operator can run cluster-wide or with `WATCH_NAMESPACES` set to a comma
separated namespace list. Cluster-wide mode is the simplest shared-operator
shape.

When watch scope is restricted, a `Tamoss` resource outside that namespace set
is ignored. The operator logs the ignored namespace and does not update that
resource's status. If an instance does not reconcile, check:

```bash
kubectl -n tamoss-system get deploy operator-controller-manager -o jsonpath='{.spec.template.spec.containers[0].env}'
kubectl -n <tenant-namespace> get tamoss
kubectl -n tamoss-system logs deploy/operator-controller-manager --tail=200
```

## Multiple Instances

Multiple `Tamoss` resources can share platform services when their names,
namespaces, hostnames, and referenced Secrets do not conflict.

For Gateway API routing, duplicate hostname or route admission failures surface
through `Tamoss.status.conditions[HostnamesReady]`. The operator relies on the
Gateway implementation for hostname admission; it does not maintain a separate
cluster-wide hostname registry.
