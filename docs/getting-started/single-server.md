# Single Server

Use `single-server` when TAMOSS runs on one Kubernetes node or a small
self-managed cluster and should still use the same operator-managed backend
shape as the larger profile.

## Requirements

- A conformant Kubernetes cluster.
- A working kubeconfig with permissions to apply platform, operator, and
  instance resources.
- A default StorageClass or explicit storage classes in the `Tamoss` CR.
- DNS and TLS planning for API, UI, S3, and Authentik hostnames.

## Install

For local validation on Kind:

```bash
task kind:up PROFILE=single-server
```

This validates the `single-server` profile shape on Kind; it is not the
existing-cluster install path.

For an existing cluster:

```bash
export KUBECONFIG=/path/to/kubeconfig

task env:init NAME=my-single-server PROFILE=single-server DOMAIN=tamoss.example.com
$EDITOR deploy/environments/my-single-server/platform-values.yaml
$EDITOR deploy/environments/my-single-server/tamoss-patch.yaml
task env:apply ENV=my-single-server KUBECONFIG="$KUBECONFIG"
task env:wait ENV=my-single-server KUBECONFIG="$KUBECONFIG"
task env:summary ENV=my-single-server KUBECONFIG="$KUBECONFIG"
```

## Configure

Start from a generated environment composition under `deploy/environments/<name>`.
Use `platform-values.yaml` to select platform components, and set a public base
domain unless you configure every public endpoint directly.

The operator derives `api`, `app`, `s3`, and `auth` hostnames from that base
domain and applies profile defaults. The remote profile defaults to
`ClusterIssuer/tamoss-public`; set the ACME email in `platform-values.yaml`, or
switch `tls.mode` to `existing`/`disabled` when certificate ownership is outside
the TAMOSS platform layer. Override normal `Tamoss` YAML fields directly in
`tamoss-patch.yaml` when you need different provider ownership, resources,
storage, or routing.

## Key Settings

### One replica of everything

The profile runs one replica each of API, worker, and UI, one CNPG PostgreSQL
instance, and one RustFS server. There are no PodDisruptionBudgets or
anti-affinity rules; a node drain takes the service down until pods
reschedule. That trade is the point of the profile.

### Scaling up without scaling out

Raise the resources of the single pods in `tamoss-patch.yaml` when the
defaults (API 384Mi request/768Mi limit, worker 128Mi/384Mi) run short:

```yaml
  api:
    resources:
      requests:
        cpu: 500m
        memory: 768Mi
      limits:
        cpu: "2"
        memory: 1536Mi
  backends:
    db:
      cnpg:
        resources:
          requests:
            memory: 1Gi
          limits:
            memory: 2Gi
```

When one node stops being enough, move to `multi-server` rather than adding
replicas here: the HA behaviours (PDBs, anti-affinity, replicated CNPG) are
profile defaults there, not bolt-ons.

## Validate

```bash
task e2e:deployed PROFILE=single-server KUBECONFIG="$KUBECONFIG"
```

See also:

- [Profiles](../concepts/profiles.md)
- [Provider Ownership](../concepts/provider-ownership.md)
- [Install](../operations/install.md)
