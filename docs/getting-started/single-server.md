# Single Server

Use `single-server` when TAMOSS runs on one Kubernetes node or a small
self-managed cluster and should still use the same operator-managed backend
shape as the larger profile.

## Cluster Requirements

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

task k8s:init NAME=my-single-server PROFILE=single-server DOMAIN=tamoss.example.com
$EDITOR deploy/environments/my-single-server/tamoss-patch.yaml
task k8s:apply ENV=my-single-server KUBECONFIG="$KUBECONFIG"
task k8s:wait ENV=my-single-server KUBECONFIG="$KUBECONFIG"
```

## Configure

Start from a generated environment overlay under `deploy/environments/<name>`.
Set a public base domain unless you configure every public endpoint directly.

The operator derives `api`, `app`, `s3`, and `auth` hostnames from that base
domain and applies profile defaults. Override normal `Tamoss` YAML fields
directly in `tamoss-patch.yaml` when you need different resources, storage, or
routing.

See also:

- [Profiles](../concepts/profiles.md)
- [Provider Ownership](../concepts/provider-ownership.md)
- [Install](../operations/install.md)
