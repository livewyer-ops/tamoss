# Deployment Profiles

Profiles are named default sets for common infrastructure shapes. They are not
separate products and they do not change the operator install path.

These are deployment profiles. TAMS [Flow Profiles](flow-profiles.md) are a
separate media-metadata capability.

| Profile | Purpose | Backing services |
| --- | --- | --- |
| `local-kind` | Local evaluation and development on [Kind](https://kind.sigs.k8s.io/). | [CNPG](https://cloudnative-pg.io/) and [RustFS](https://github.com/rustfs/rustfs) Operator with small single-node defaults, [Authentik](https://goauthentik.io/), [cert-manager](https://cert-manager.io/), and [Traefik](https://traefik.io/) on host port 443. |
| `edge` | Single-node ARM64 Kubernetes installs with local persistent storage, sized from a Raspberry Pi 4 Model B 4 GB floor. | CNPG and RustFS Operator with compact single-node defaults, bearer-token auth by default or on-node managed Authentik, cert-manager, and Traefik. |
| `single-server` | One Kubernetes node or a small self-managed cluster. | CNPG and RustFS Operator with single-node durable defaults plus shared Authentik, cert-manager, and Traefik. |
| `multi-server` | Production reference for multi-node Kubernetes. | Replicated workloads, CNPG, RustFS Operator, shared Authentik, cert-manager, and Traefik. |

Every checked-in instance manifest sets `.spec.profile`. The operator fills in
defaults for that profile, then explicit YAML fields in the `Tamoss` CR override
those defaults.

Profiles do not define tenant identity. In shared clusters, tenant boundaries
come from Kubernetes namespaces and the `Tamoss` resources applied in them.

`local-kind`, `single-server`, and `multi-server` default application
authentication to managed Authentik. Omitting `spec.auth` selects
`auth.providedBy: authentik-blueprints`, derives `https://auth.<baseDomain>`
from `spec.publicEndpoint.baseDomain`, and expects the platform Authentik
install in the `auth` namespace. Use an explicit `spec.auth.providedBy:
external` or `spec.auth.providedBy: none` only when an environment intentionally
opts out of the profile default.

`edge` does not default to Authentik. It selects `auth.providedBy: external`
with OAuth2 disabled and uses the generated TAMOSS API token Secret for
bearer-token access. Declaring `spec.auth.providedBy: authentik-blueprints`
opts the install into the managed Authentik stack on the same node; measured
on a 4 GB ARM64 node, the OAuth mode holds roughly 2.8 GiB of memory at
steady state against 2.3 GiB for token mode. Set `spec.auth.providedBy: none`
only when the edge install should accept anonymous API requests.

`deploy/profiles.yaml` is the profile registry used by Taskfile helpers. Each
supported profile records its Kind environment composition, target environment
file, and the Kind cluster config used for local validation.

`local-kind` is a local test adapter. It creates a Kind cluster, loads local
development images, uses `localtest.me`, and exists to validate the operator
flow quickly. `edge` is a remote-capable single-node ARM64 profile for nodes
as small as a Raspberry Pi 4 Model B with 4 GB RAM; running
`task kind:up PROFILE=edge` validates the profile shape on Kind, but the target
install path is a normal ARM64 Kubernetes cluster such as
[K3s](https://k3s.io/). `single-server`
is the remote-capable single-node or small-cluster profile. Running
`task kind:up PROFILE=single-server` validates that profile on Kind; it is not
the remote install path.

Running `task kind:up PROFILE=multi-server` validates the production reference
profile on a disposable multi-node Kind cluster. That local harness exercises
multi-node scheduling shape, but the production `multi-server` install path
remains normal Kubernetes through the `env:*` tasks.

## Security Defaults

`edge`, `single-server`, and `multi-server` default TAMOSS application workloads to a
restricted-compatible security posture: non-root pods and containers, runtime
default seccomp, no privilege escalation, and dropped Linux capabilities. The
operator manager keeps the stricter read-only root filesystem default; API, UI,
and worker images do not default to read-only root filesystems until their
runtime write paths are verified.

`multi-server` adds production-oriented resource requests,
PodDisruptionBudgets, preferred pod anti-affinity, and NetworkPolicies. The
NetworkPolicy defaults allow only the traffic TAMOSS commonly needs: ingress
from routing or monitoring systems to exposed component ports, DNS egress, and
egress to HTTP/TLS services, PostgreSQL, and S3-compatible storage. A CNI that
enforces NetworkPolicy is required for those restrictions to take effect.
UI egress is destination-scoped to the instance API and Console, the managed
Authentik server, and cluster DNS. An enabled Console additionally requires
explicit Kubernetes Service and API-server endpoint IP blocks; it never
defaults to arbitrary HTTPS egress.

On Cilium clusters with a self-hosted API server, enable
`policyCIDRMatchMode: nodes` so standard `NetworkPolicy.ipBlock` peers can match
the configured control-plane node CIDRs. Without that Cilium setting, Console
fails closed because its Kubernetes watch traffic is denied.

## Routing

`local-kind` uses Kubernetes `Ingress` through Traefik so it can run on a plain
Kind cluster with port 443 exposed on the host.

For production-style `multi-server` clusters, use Gateway API `HTTPRoute` when
the platform already provides Gateway API CRDs and a Gateway controller. The
operator renders application routes only. It does not install Gateway API CRDs,
GatewayClasses, Gateways, or a Gateway controller.

When `spec.httpRoute.enabled` is true, route readiness is reported through
`Tamoss.status.conditions[RoutingReady]`. Hostname acceptance and duplicate
hostname conflicts are reported through
`Tamoss.status.conditions[HostnamesReady]`.

## Public Endpoints

Set one base domain when the standard host shape is acceptable:

```yaml
spec:
  profile: multi-server
  publicEndpoint:
    baseDomain: tamoss.example.com
```

The operator derives:

- API: `https://api.<baseDomain>`
- UI: `https://app.<baseDomain>`
- S3: `https://s3.<baseDomain>`
- Authentik: `https://auth.<baseDomain>` for Authentik-backed profiles

Override a specific endpoint only when that endpoint must deviate from the
standard shape.

## TLS Defaults

`local-kind` defaults to `ClusterIssuer/tamoss-selfsigned` and local test TLS
Secret names. `edge` defaults to `ClusterIssuer/tamoss-edge-selfsigned` with
edge TLS Secret names. `single-server` and `multi-server` default to
`ClusterIssuer/tamoss-public` with public TLS Secret names. The matching
single-server and multi-server platform values use `tls.mode: public` to render
an ACME ClusterIssuer for remote environments:

```yaml
tls:
  mode: public
  issuerName: tamoss-public
  acme:
    email: ops@example.com
```

Environment compositions only need to override this when the cluster already
provides cert-manager, a different ClusterIssuer name, or pre-created TLS
Secrets. Explicit `spec.ingress.annotations` in the `Tamoss` CR are preserved
instead of receiving the profile default.

## Validation Adapters

Kind-specific environments, such as `deploy/environments/multi-server`, are
test adapters for these public profiles, not additional deployment profiles.
The `multi-server` adapter uses one control-plane node and three workers to
exercise its scheduling shape.

For the runnable profile matrix and its local network exposure, use the
[Profile Matrix](../development/testing.md#profile-matrix) development guide.
