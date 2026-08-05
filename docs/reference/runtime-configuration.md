# Runtime Configuration

Runtime configuration covers Kubernetes workload overrides, image selection,
runtime environment variables, and network policy toggles. Keep provider
ownership decisions in the `Tamoss` CR and make durable changes in the
environment overlay under `deploy/environments/<name>`.

## Workload Overrides

Override normal Kubernetes workload fields directly:

```yaml
spec:
  api:
    replicaCount: 3
    resources:
      requests:
        cpu: 500m
        memory: 512Mi
  worker:
    replicaCount: 3
  ui:
    replicaCount: 2
```

Use `.spec.api`, `.spec.worker`, and `.spec.ui` for resources, scheduling,
labels, annotations, probes, environment variables, volumes, and security
context.

`edge`, `single-server`, and `multi-server` default API, worker, and UI pods
to run as
non-root with runtime default seccomp, no privilege escalation, and dropped
Linux capabilities. Override `podSecurityContext` or `securityContext` only
when a workload image requires a different setting.

`multi-server` enables default NetworkPolicies. Disable them with
`spec.networkPolicy.enabled: false`, or replace the per-component ingress and
egress rules under `spec.networkPolicy.api`, `spec.networkPolicy.worker`, and
`spec.networkPolicy.ui`.

## Image Overrides

The operator applies image defaults for a minimal CR and reports the effective
values under `.status.resolved.images`. Override images directly when a cluster
needs an internal registry, pinned digest, or tested component build:

```yaml
spec:
  api:
    image:
      repository: registry.example.com/tamoss-api
      tag: v0.4.0
  ui:
    image:
      repository: registry.example.com/tamoss-ui
      tag: v0.4.0
  images:
    schemaMigrationPostgresClient: registry.example.com/postgres:18-alpine
```

The API image also carries the TAMOSS database migration CLI used by the
operator schema Job. `schemaMigrationPostgresClient` remains the helper image
for storage-backend database registration Jobs. Managed provider images are
configured where the provider is selected. For example,
[CNPG](https://cloudnative-pg.io/) PostgreSQL uses
`.spec.backends.db.cnpg.postgresVersion` and
[RustFS](https://github.com/rustfs/rustfs) uses
`.spec.backends.s3.rustfsOperator.image`.

Set API and UI image tags explicitly in non-local overlays. If a tag is omitted,
the operator resolves the image to the repository development tag (`dev`) rather
than letting Kubernetes implicitly pull `latest`.

Platform controllers such as [cert-manager](https://cert-manager.io/),
[Traefik](https://traefik.io/), [Authentik](https://goauthentik.io/), CNPG
Operator,
and RustFS Operator are installed before TAMOSS and are versioned in the
platform dependency source, not in the `Tamoss` CR.

## Advanced Resource Overrides

When a provider CRD exposes a field that TAMOSS does not model directly, use
`.spec.advanced.resourcePatches` to patch the emitted resource before the
operator applies it. Use `.spec.advanced.extraResources` for additional
Kubernetes resources that should share the `Tamoss` instance lifecycle.

Advanced YAML is intentionally operator-owned: keep it close to the environment
overlay and review it when the referenced provider CRD is upgraded.

## Runtime Environment Variables

In Kubernetes, the operator renders API and worker environment variables from
the `Tamoss` CR and referenced Secrets. Prefer changing the environment overlay
or referenced Secrets rather than editing Deployments directly.

For API runtime variable names, see `src/app/tamoss/settings.py`.

Runtime boolean, integer, duration-in-seconds, URL, and comma-separated list
settings are parsed by the shared settings boundary. Unset values use the
documented defaults, but invalid explicit values fail startup with the setting
name in the error. This applies equally to API and worker pods, including
operator-rendered values such as webhook policy, OAuth2, S3 timeout, and worker
poll settings.

PostgreSQL URLs derived from `POSTGRES_HOST`, `POSTGRES_USER`,
`POSTGRES_PASSWORD`, `POSTGRES_DB`, and `POSTGRES_PORT` percent-encode the user,
password, and database components. Prefer these component variables when the
operator owns database credentials.

Forward-auth identity headers are only trusted when
`TAMOSS_TRUST_FORWARD_AUTH_HEADERS=true` and the proxy sends
`X-TAMOSS-Forward-Auth-Secret` matching `TAMOSS_FORWARD_AUTH_SHARED_SECRET` or
`TAMOSS_FORWARD_AUTH_SHARED_SECRET_FILE`. Basic auth is only enabled when
`TAMOSS_BASIC_AUTH_PASSWORD` or `TAMOSS_BASIC_AUTH_PASSWORD_FILE` is configured;
`TAMOSS_API_TOKEN` is used for Bearer token authentication only.

Webhook targets are treated as outbound egress policy. TAMOSS rejects loopback,
link-local, private, Kubernetes service DNS, and cloud metadata targets by
default, and validates again in the worker before delivery. To allow a private
receiver, configure both the API and worker with
`TAMOSS_WEBHOOK_ALLOWED_HOSTS` as a comma-separated list of exact hostnames, IPs,
CIDRs, or leading-dot DNS suffixes. `TAMOSS_WEBHOOK_ALLOW_PRIVATE_TARGETS=true`
allows private addresses generally, but metadata and Kubernetes service targets
still require an explicit host allowlist. With the operator, set the same values
under `.spec.api.env` and `.spec.worker.env` so registration and delivery use the
same policy.
