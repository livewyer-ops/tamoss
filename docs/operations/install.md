# Install

TAMOSS installs through source-controlled Kustomize inputs. For existing
clusters, create an environment overlay, edit the generated YAML, then apply it:

```bash
export KUBECONFIG=/path/to/kubeconfig

task k8s:init NAME=my-prod PROFILE=multi-server DOMAIN=tamoss.example.com
$EDITOR deploy/environments/my-prod/tamoss-patch.yaml
task k8s:apply ENV=my-prod KUBECONFIG="$KUBECONFIG"
task k8s:wait ENV=my-prod KUBECONFIG="$KUBECONFIG"
```

The task workflow applies three ordered layers. The platform layer installs
shared prerequisites. The operator layer installs the TAMOSS CRDs, controller,
RBAC, and webhooks. The environment layer applies one or more namespaced
`Tamoss` custom resources.

For multiple tenant namespaces, install the platform and operator once, then
apply namespace-local `Tamoss` and `StorageBackend` resources in each tenant
namespace.

Storage provisioning is `StorageBackend`-driven. The operator creates the
default `StorageBackend` for each `Tamoss` instance and reconciles additional
`StorageBackend` resources for extra registered TAMS storage backends. API and
worker pods consume the registered metadata and mounted runtime credentials;
they do not create storage backend rows during Kubernetes startup.

## Operator Runtime

The checked-in operator install includes leader election, voluntary leader
release on shutdown, readiness/liveness probes, a startup probe for slower
initial cache and webhook setup, and bounded Authentik HTTP clients. The
manager defaults reserve `500m` CPU and `256Mi` memory with a `1` CPU and
`512Mi` limit; tune these through a Kustomize patch only after checking
operator memory and CPU under your expected number of `Tamoss` resources.

## Local Convenience Path

Use `task kind:up` for local Kind evaluation and validation:

```bash
task kind:up PROFILE=local-kind
```

Supported profiles are:

- `local-kind`
- `single-server`
- `multi-server`

When `PROFILE=single-server` or `PROFILE=multi-server` is used with `task kind:up`,
the task uses Kind-compatible platform overlays while keeping the public
instance profile name. `PROFILE=multi-server` also uses the checked-in
multi-node Kind configuration so local validation has separate worker nodes; the
remote install path still uses normal Kubernetes overlays.

## Existing Cluster

The generated environment is a normal Kustomize overlay. It starts from
`deploy/instances/<profile>` and patches the `Tamoss` CR directly; there is no
separate values file or renderer. Treat this overlay as the durable source of
configuration for endpoint, provider, resource, replica, and routing changes.

If automation cannot call Task, keep the same checked-in Kustomize inputs and
apply the same layers in order:

```bash
kubectl --kubeconfig "$KUBECONFIG" apply -k deploy/platform/<profile>
kubectl --kubeconfig "$KUBECONFIG" apply --server-side -k deploy/operator
kubectl --kubeconfig "$KUBECONFIG" apply -k deploy/environments/<name>
```

## Expected Signals

```bash
task k8s:status ENV=my-prod KUBECONFIG="$KUBECONFIG"
```

`Ready=True` on the `Tamoss` resource is the primary success signal. If the
instance is not ready, read status conditions before looking at individual pods.
For Gateway API installs, also check `kubectl -n tams get httproute` and the
`RoutingReady` and `HostnamesReady` conditions on the `Tamoss` resource.

## Maintainer Platform Vendoring

Runtime installs use checked-in Kustomize manifests. Maintainer-only vendor
tasks refresh checked-in platform manifests from `deploy/platform/dependencies.yaml`.
Normal installs do not run live chart installs and do not apply remote URLs.

See also:

- [Profiles](../concepts/profiles.md)
- [Provider Ownership](../concepts/provider-ownership.md)
- [Troubleshooting](troubleshooting.md)
