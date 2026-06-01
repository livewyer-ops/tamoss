# Profiles

Profiles are named default sets for common infrastructure shapes. They are not
separate products and they do not change the operator install path.

| Profile | Purpose | Backing services |
| --- | --- | --- |
| `local-kind` | Local evaluation and development on Kind. | CNPG and RustFS Operator with small single-node defaults, Authentik, cert-manager, and Traefik on host port 443. |
| `single-server` | One Kubernetes node or a small self-managed cluster. | CNPG and RustFS Operator with single-node durable defaults plus shared Authentik, cert-manager, and Traefik. |
| `multi-server` | Production reference for multi-node Kubernetes. | Replicated workloads, CNPG, RustFS Operator, shared Authentik, cert-manager, and Traefik. |

Every checked-in instance manifest sets `.spec.profile`. The operator fills sane
defaults for that profile, then explicit YAML fields in the `Tamoss` CR override
those defaults.

Profiles do not define tenant identity. In shared clusters, tenant boundaries
come from Kubernetes namespaces and the `Tamoss` resources applied in them.

All profiles default application authentication to managed Authentik. Omitting
`spec.auth` selects `auth.providedBy: authentik-blueprints`, derives
`https://auth.<baseDomain>` from `spec.publicEndpoint.baseDomain`, and expects
the platform Authentik install in the `auth` namespace. Use an explicit
`spec.auth.providedBy: external` or `spec.auth.providedBy: none` only when an
environment intentionally opts out of the profile default.

`deploy/profiles.yaml` is the profile registry used by Taskfile helpers. Each
supported profile records its Kind environment composition, target environment
file, and the Kind cluster config used for local validation.

`local-kind` is a local test adapter. It creates a Kind cluster, loads local
development images, uses `localtest.me`, and exists to validate the operator
flow quickly. `single-server` is the remote-capable single-node or small-cluster
profile. Running `task kind:up PROFILE=single-server` validates that profile on
Kind; it is not the remote install path.

Running `task kind:up PROFILE=multi-server` validates the production reference
profile on a disposable multi-node Kind cluster. That local harness exercises
multi-node scheduling shape, but the production `multi-server` install path
remains normal Kubernetes through the `env:*` tasks.

## Security Defaults

`single-server` and `multi-server` default TAMOSS application workloads to a
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
- Authentik: `https://auth.<baseDomain>`

Override a specific endpoint only when that endpoint must deviate from the
standard shape.

## TLS Defaults

`local-kind` defaults to `ClusterIssuer/tamoss-selfsigned` and local test TLS
Secret names. `single-server` and `multi-server` default to
`ClusterIssuer/tamoss-public` with public TLS Secret names. The matching
platform values use `tls.mode: public` to render an ACME ClusterIssuer for
remote environments:

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

## Local Profile Validation

Run profiles one at a time on Kind:

```bash
task kind:up PROFILE=local-kind
task kind:up PROFILE=single-server
task kind:up PROFILE=multi-server
```

Run the sequential profile gate:

```bash
task kind:profiles:e2e
```

The Kind-specific environments, such as `deploy/environments/kind-multi-server`,
are test adapters for local validation. They are not additional public profiles.
The `multi-server` local validation path also uses `deploy/kind-multi-server.yaml`
so Kind creates one control-plane node and three worker nodes while keeping host
HTTPS ingress on port 443.

Both checked-in Kind configs bind host port `443` to `0.0.0.0` for local browser
and API testing. Treat that as local development exposure: other machines on the
same network may be able to reach the test ingress while the Kind cluster is
running.
