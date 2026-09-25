# Install

TAMOSS uses native Helmfile and Kubernetes manifests for installation. Task can
create an environment directory, but the cluster resources remain ordinary
Helmfile state and Kustomize compositions that can be managed by any Kubernetes
workflow.

Choose the product release and export it as `TAMOSS_VERSION`. The generated
environment uses it for the operator install reference and initial instance.
Run `task env:init` from the source revision that publishes that release; the
same checkout supplies the platform dependency pins.
Install the [Helm diff plugin](https://github.com/databus23/helm-diff) before
using Helmfile `apply`:

```bash
helm plugin install https://github.com/databus23/helm-diff --verify=false
helm diff version
```

```bash
export KUBECONFIG=/path/to/kubeconfig

task env:init TAMOSS_VERSION="$TAMOSS_VERSION" NAME=my-prod PROFILE=multi-server DOMAIN=tamoss.example.com
$EDITOR deploy/environments/my-prod/platform-values.yaml
$EDITOR deploy/environments/my-prod/operator/defaults.yaml
(
  cd deploy/platform
  helmfile --kubeconfig "$KUBECONFIG" \
    --file helmfile.yaml.gotmpl \
    --state-values-file values/defaults.yaml \
    --state-values-file ../environments/my-prod/platform-values.yaml \
    apply \
    --skip-diff-on-install \
    --sync-args "--server-side=true" \
    --wait \
    --wait-for-jobs
)
kubectl --kubeconfig "$KUBECONFIG" apply --server-side -k deploy/environments/my-prod/operator
kubectl --kubeconfig "$KUBECONFIG" wait --for=condition=Established crd/tamosses.tamoss.livewyer.io --timeout=60s
kubectl --kubeconfig "$KUBECONFIG" -n tamoss-system rollout status deployment/operator-controller-manager --timeout=5m
kubectl --kubeconfig "$KUBECONFIG" apply -k deploy/environments/my-prod
```

Apply the layers in order. The platform layer uses
`deploy/platform/helmfile.yaml.gotmpl` to install shared prerequisites as
separate [Helm](https://helm.sh/) releases, waits for the dependency
operators, then applies
TAMOSS-owned platform configuration through `deploy/platform/charts/config`.
The platform state is built from `deploy/platform/values/defaults.yaml` plus the
environment's `platform-values.yaml`. The operator layer uses the environment's
`operator/kustomization.yaml`, which references the selected release's published
installation and mounts the installation defaults. It installs the CRDs,
controller, RBAC and webhooks. The environment layer applies one or more
namespaced `Tamoss` custom resources. `task env:apply` is an optional shortcut;
`task env:wait` and `task env:status` are optional status helpers.

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
settings to the operator. Each instance has a Kustomize directory containing a
namespace, a minimal `Tamoss` resource and any other resources owned by that
instance.

`task env:init` generates this composition:

```text
deploy/environments/<name>/
├── kustomization.yaml     # composes all instance directories
├── platform-values.yaml   # shared Helm releases
├── operator/
│   ├── kustomization.yaml # published operator and installation defaults mount
│   └── defaults.yaml      # shared profile, domain and ingress settings
└── instances/
    └── tamoss-<profile>/
        ├── kustomization.yaml
        ├── namespace.yaml
        └── tamoss.yaml    # instance identity and selected release
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
```

Review the generated storage sizes and hostnames before applying to an ARM64
single-node cluster.

## Several Instances in One Environment

An environment directory may hold several `Tamoss` instances on one cluster:
one Kustomize directory per instance, with each instance in its own namespace.
Platform components (Authentik,
[Traefik](https://traefik.io/), cert-manager,
[CNPG](https://cloudnative-pg.io/),
[RustFS](https://github.com/rustfs/rustfs) Operator) are installed once per
cluster and shared; instance
resources, buckets, and databases stay isolated per namespace.

An environment with two instances looks like this:

```text
deploy/environments/<env>/
├── kustomization.yaml         # lists each instance directory below
├── platform-values.yaml       # shared platform components, applied once
├── operator/
│   ├── kustomization.yaml     # shared operator installation
│   └── defaults.yaml          # inherited instance settings
├── instances/
│   ├── prod-a/
│   │   ├── kustomization.yaml # resources owned by prod-a
│   │   ├── namespace.yaml
│   │   └── tamoss.yaml
│   └── prod-b/
│       ├── kustomization.yaml
│       ├── namespace.yaml
│       └── tamoss.yaml
└── monitoring/
    └── prod-a/                # optional per-instance dashboards and alerts
```

Add an instance with `task env:instance:init`, which writes its Kustomize
directory and registers it in the root composition:

```bash
task env:instance:init TAMOSS_VERSION="$TAMOSS_VERSION" ENV=<env> INSTANCE=prod-b
```

The namespace defaults to the instance name. The resource contains only
`spec.version` unless optional `PROFILE` or `DOMAIN` overrides are supplied.
Use `NAMESPACE` to select another namespace.

Apply one instance and its namespace-scoped resources with:

```bash
kubectl --kubeconfig "$KUBECONFIG" apply -k deploy/environments/<env>/instances/prod-b
task env:wait ENV=<env> INSTANCE=prod-b KUBECONFIG="$KUBECONFIG"
```

Add `StorageBackend`, `FlowProfile` or other instance resources to that
directory and list them in its `kustomization.yaml`. `kubectl apply -k` then
applies only the selected instance's resources. Give each instance its own
namespace.

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
`task kind:up PROFILE=local-kind` prints local credentials with its summary.
Remote `task env:summary` output contains URLs and lifecycle status but no
secret values. Retrieve them when needed with
`task env:credentials ENV=<env> INSTANCE=<name> KUBECONFIG="$KUBECONFIG"`.
Environment files carry secret material only when operators choose to set
those values explicitly; in that case keep the environment directory out of
version control, restrict file permissions, and take care with broad staging
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
