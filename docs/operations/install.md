# Install

TAMOSS installs through source-controlled environment inputs. For existing
clusters, create an environment composition, edit the generated platform values
and `Tamoss` YAML, then apply it:

```bash
export KUBECONFIG=/path/to/kubeconfig

task env:init NAME=my-prod PROFILE=multi-server DOMAIN=tamoss.example.com
$EDITOR deploy/environments/my-prod/platform-values.yaml
$EDITOR deploy/environments/my-prod/tamoss-patch.yaml
task env:apply ENV=my-prod KUBECONFIG="$KUBECONFIG"
task env:wait ENV=my-prod KUBECONFIG="$KUBECONFIG"
```

The task workflow applies ordered layers. The platform layer uses
`deploy/platform/helmfile.yaml.gotmpl` to install shared prerequisites as
separate [Helm](https://helm.sh/) releases, waits for the dependency
operators, then applies
TAMOSS-owned platform configuration through `deploy/platform/charts/config`.
The platform state is built from `deploy/platform/values/defaults.yaml` plus the
environment's `platform-values.yaml`. The operator layer installs the TAMOSS
CRDs, controller, RBAC, and webhooks. The environment layer applies one or more
namespaced `Tamoss` custom resources.

For multiple tenant namespaces, install the platform and operator once, then
apply namespace-local `Tamoss`, `StorageBackend`, and optional `FlowProfile`
resources in each tenant
namespace.

Storage provisioning is `StorageBackend`-driven. The operator creates the
default `StorageBackend` for each `Tamoss` instance and reconciles additional
`StorageBackend` resources for extra registered TAMS storage backends. API and
worker pods consume the registered metadata and mounted runtime credentials;
they do not create storage backend rows during Kubernetes startup.

## Operator Runtime

The checked-in operator install includes leader election, voluntary leader
release on shutdown, readiness/liveness probes, a startup probe for slower
initial cache and webhook setup, and bounded
[Authentik](https://goauthentik.io/) HTTP clients. The
manager defaults reserve `500m` CPU and `256Mi` memory with a `1` CPU and
`512Mi` limit; tune these through a [Kustomize](https://kustomize.io/) patch
only after checking
operator memory and CPU under your expected number of `Tamoss` resources.

## Local Convenience Path

Use `task kind:up` for local Kind evaluation and validation:

```bash
task kind:up PROFILE=local-kind
```

Supported profiles are:

- `local-kind`
- `edge`
- `single-server`
- `multi-server`

When `PROFILE=edge`, `PROFILE=single-server`, or `PROFILE=multi-server` is used
with `task kind:up`, the task uses the matching Kind environment under
`deploy/environments/` while keeping the public instance profile name.
`PROFILE=multi-server` also uses the checked-in multi-node Kind configuration so
local validation has separate worker nodes; the remote install path still uses
normal environment compositions.

## Existing Cluster

The generated environment is the composition root. `platform-values.yaml`
selects which platform components Helm installs. The Kustomize overlay starts
from `deploy/instances/<profile>` and patches the `Tamoss` CR directly. Treat
both files as the durable source of configuration for provider ownership,
endpoints, resources, replicas, and routing.

`task env:init` generates this composition:

```text
deploy/environments/<name>/
├── kustomization.yaml     # overlay: deploy/instances/<profile> plus the patch
├── platform-values.yaml   # platform component selection for the Helmfile layer
└── tamoss-patch.yaml      # instance overrides: profile and public base domain
```

The patch keeps the instance name `tamoss-<profile>` and the `tams`
namespace from the checked-in instance manifest. A `multi-server`
environment therefore contains the instance `tamoss-multi-server` in the
`tams` namespace.

Generated remote environments default to trusted public TLS. The platform
[Helmfile](https://helmfile.readthedocs.io/)
creates `ClusterIssuer/tamoss-public` when `tls.mode: public` is selected:

```yaml
tls:
  mode: public
  issuerName: tamoss-public
  acme:
    email: ops@example.com
```

Use `tls.mode: existing` when [cert-manager](https://cert-manager.io/) and
the ClusterIssuer are managed
outside the TAMOSS platform layer. Use `tls.mode: disabled` when TLS Secrets are
pre-created and cert-manager annotations should be omitted from explicit
`Tamoss` ingress overrides.

`PROFILE=edge` generates a self-signed, Authentik-free platform composition from
`deploy/platform/values/edge-reference.yaml`:

```bash
task env:init NAME=my-edge PROFILE=edge DOMAIN=tamoss.edge
task env:apply ENV=my-edge KUBECONFIG="$KUBECONFIG"
```

Review the generated storage sizes and hostnames before applying to an ARM64
single-node cluster.

If automation cannot call Task, keep the same checked-in inputs and apply the
same layers in order:

```bash
(
  cd deploy/platform
  helmfile --kubeconfig "$KUBECONFIG" \
    --file helmfile.yaml.gotmpl \
    --state-values-file values/defaults.yaml \
    --state-values-file ../../deploy/environments/<name>/platform-values.yaml \
    sync \
    --sync-args "--server-side=true --rollback-on-failure" \
    --wait \
    --wait-for-jobs
)
kubectl --kubeconfig "$KUBECONFIG" apply --server-side -k deploy/operator
kubectl --kubeconfig "$KUBECONFIG" apply -k deploy/environments/<name>
```

## Several Instances in One Environment

An environment directory may hold several `Tamoss` instances on one cluster:
one file per instance plus a shared `kustomization.yaml`, with each instance
in its own namespace. Platform components (Authentik,
[Traefik](https://traefik.io/), cert-manager,
[CNPG](https://cloudnative-pg.io/),
[RustFS](https://github.com/rustfs/rustfs) Operator) are installed once per
cluster and shared; instance
resources, buckets, and databases stay isolated per namespace.

An environment with two instances looks like this:

```text
deploy/environments/<env>/
├── kustomization.yaml         # lists every instance manifest below
├── platform-values.yaml       # shared platform components, applied once
├── prod-a.yaml                # Tamoss CR in namespace prod-a
├── prod-a-storage.yaml        # default StorageBackend for prod-a
├── prod-b.yaml                # Tamoss CR in namespace prod-b
└── monitoring/
    └── prod-a/                # optional per-instance dashboards and alerts
```

Add an instance with `task env:instance:init`, which writes the manifest and
registers it in `kustomization.yaml`:

```bash
task env:instance:init ENV=<env> INSTANCE=prod-b PROFILE=multi-server \
  DOMAIN=prod-b.example.com NAMESPACE=prod-b
```

`task env:instance:apply` then applies the whole kustomization. Instances
using `s3.providedBy: external` need their default `StorageBackend` manifest
alongside the CR, as `prod-a-storage.yaml` shows.

`env:wait`, `env:status`, and `env:summary` report on every instance in the
environment. Pass `INSTANCE=<name>` to work with one:

```bash
task env:summary ENV=<env> INSTANCE=prod-a KUBECONFIG="$KUBECONFIG"
```

## Environment Secrets

The platform Authentik flow needs no secrets in the environment files: when
`platform-values.yaml` leaves the Authentik secret key, bootstrap
credentials, and database passwords unset, the platform chart generates that
material in-cluster on first apply and preserves it across later applies.
`task env:summary` prints the resolved admin credentials. Environment files
carry secret material only when operators choose to set those values
explicitly; in that case keep the environment directory out of version
control, restrict file permissions, and take care with broad staging
commands such as `git add -A` in a worktree that contains live environment
directories.

## Expected Signals

```bash
task env:status ENV=my-prod KUBECONFIG="$KUBECONFIG"
```

`Ready=True` on the `Tamoss` resource is the primary success signal. If the
instance is not ready, read status conditions before looking at individual pods.
For Gateway API installs, also check `kubectl -n tams get httproute` and the
`RoutingReady` and `HostnamesReady` conditions on the `Tamoss` resource.

## Platform Inputs

Runtime installs use the checked-in Helmfile platform state plus checked-in
Kustomize manifests for the operator and `Tamoss` resources. Platform dependency
versions are pinned in `deploy/platform/helmfile.yaml.gotmpl` and recorded in
`deploy/platform/dependencies.yaml`.

See also:

- [Profiles](../concepts/profiles.md)
- [Provider Ownership](../concepts/provider-ownership.md)
- [Troubleshooting](troubleshooting.md)
