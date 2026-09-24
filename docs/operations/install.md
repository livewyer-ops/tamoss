# Install

TAMOSS installs through source-controlled environment inputs. For existing
clusters, create an environment composition, review the generated platform
values and operator defaults, then apply it:

Set `TAMOSS_VERSION` to the exact release to install. The generated environment
pins its operator installation and each instance independently.

```bash
export KUBECONFIG=/path/to/kubeconfig

task env:init TAMOSS_VERSION="$TAMOSS_VERSION" NAME=my-prod PROFILE=multi-server DOMAIN=tamoss.example.com
$EDITOR deploy/environments/my-prod/platform-values.yaml
$EDITOR deploy/environments/my-prod/operator/defaults.yaml
task env:apply ENV=my-prod KUBECONFIG="$KUBECONFIG"
task env:wait ENV=my-prod KUBECONFIG="$KUBECONFIG"
```

The task workflow applies ordered layers. The platform layer uses
`deploy/platform/helmfile.yaml.gotmpl` to install shared prerequisites as
separate [Helm](https://helm.sh/) releases, waits for the dependency
operators, then applies
TAMOSS-owned platform configuration through `deploy/platform/charts/config`.
The platform state is built from `deploy/platform/values/defaults.yaml` plus the
environment's `platform-values.yaml`. The operator layer uses the environment's
`operator/kustomization.yaml`, which references the selected release's published
installation and catalogue and mounts the installation defaults. It installs
the CRDs, controller, RBAC and webhooks. The environment layer applies one or more
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
selects shared platform components. `operator/defaults.yaml` supplies site
settings to the operator. `tamoss-patch.yaml` is a complete instance resource
containing its identity and `spec.version`; add fields there only when the
instance needs overrides.

`task env:init` generates this composition:

```text
deploy/environments/<name>/
├── kustomization.yaml     # instance resources
├── namespace.yaml
├── platform-values.yaml   # shared Helm releases
├── operator/
│   ├── kustomization.yaml # published operator and installation defaults mount
│   └── defaults.yaml     # shared profile, domain and ingress settings
└── tamoss-patch.yaml      # instance identity and release
```

The generated instance is named `tamoss-<profile>` in namespace `tams`.
For `PROFILE=multi-server DOMAIN=example.com`, create DNS records for
`api.tamoss-multi-server.tams.example.com`,
`app.tamoss-multi-server.tams.example.com`,
`s3.tamoss-multi-server.tams.example.com` and shared `auth.example.com`.
See [installation defaults](../configuration.md#installation-defaults) for
inheritance and explicit hostname overrides.

Generated single-server and multi-server environments default to public TLS.
The platform [Helmfile](https://helmfile.readthedocs.io/)
creates `ClusterIssuer/tamoss-public` when `tls.mode: public` is selected:

```yaml
tls:
  mode: public
  issuerName: tamoss-public
  acme:
    email: ops@example.com
```

Keep `operator/defaults.yaml`'s `clusterIssuer` equal to the platform's
`tls.issuerName`. The platform Authentik ingress host must also match the shared
Auth URL, normally `auth.<installation-base-domain>`. These files configure
separate layers; changing one does not update the other.

Use `tls.mode: existing` when [cert-manager](https://cert-manager.io/) and
the named ClusterIssuer are managed outside the TAMOSS platform layer.
For pre-created TLS Secrets, use `tls.mode: disabled`, set
`spec.ingress.annotations: {}` to suppress issuer defaults, and name the
instance's Secrets in `spec.publicEndpoint.tlsSecretName` and
`spec.publicEndpoint.s3TLSSecretName`. Managed Authentik needs its own platform
ingress TLS Secret.

`PROFILE=edge` generates a self-signed, Authentik-free platform composition from
`deploy/platform/values/edge-reference.yaml`:

```bash
task env:init TAMOSS_VERSION="$TAMOSS_VERSION" NAME=my-edge PROFILE=edge DOMAIN=tamoss.edge
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
    --sync-args "--server-side=true" \
    --wait \
    --wait-for-jobs
)
kubectl --kubeconfig "$KUBECONFIG" apply --server-side -k deploy/environments/<name>/operator
kubectl --kubeconfig "$KUBECONFIG" wait --for=condition=Established crd/tamosses.tamoss.livewyer.io --timeout=60s
kubectl --kubeconfig "$KUBECONFIG" -n tamoss-system rollout status deployment/operator-controller-manager --timeout=5m
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
├── operator/
│   ├── kustomization.yaml     # shared operator installation
│   └── defaults.yaml          # inherited instance settings
├── prod-a.yaml                # Tamoss CR in namespace prod-a
├── prod-a-storage.yaml        # default StorageBackend for prod-a
├── prod-b.yaml                # Tamoss CR in namespace prod-b
└── monitoring/
    └── prod-a/                # optional per-instance dashboards and alerts
```

Add an instance with `task env:instance:init`, which writes the manifest and
registers it in `kustomization.yaml`:

```bash
task env:instance:init TAMOSS_VERSION="$TAMOSS_VERSION" ENV=<env> INSTANCE=prod-b
```

The namespace defaults to the instance name. The resource contains only
`spec.version` unless optional `PROFILE` or `DOMAIN` overrides are supplied.
Use `NAMESPACE` to select another namespace.

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

`task env:wait` waits for the observed generation, selected release and
installation defaults revision before checking `Ready=True`. If the instance
is not ready, read status conditions before looking at individual pods.
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
