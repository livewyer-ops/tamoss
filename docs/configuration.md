# Configuration

Configuration starts from the `Tamoss` custom resource. The operator applies
profile defaults, then explicit YAML fields in the CR override those defaults.
For existing clusters, make durable changes in the generated environment
overlay under `deploy/environments/<name>` and reapply the task workflow.

Use this page as the routing point for configuration work. Field-level details
belong in the CR references and the canonical CRD schemas under
`operator/config/crd/bases/`.

## Common Paths

| Need | Use |
| --- | --- |
| Choose `local-kind`, `edge`, `single-server`, or `multi-server` | [Profiles](concepts/profiles.md) |
| Configure managed or external providers | [Provider Ownership](concepts/provider-ownership.md) |
| Configure storage backends and controlled storage allocation | [Storage Backends](concepts/storage-backends.md) |
| Override runtime workload, image, and environment settings | [Runtime Configuration](reference/runtime-configuration.md) |
| Rotate or mount sensitive values | [Secret Rotation](operations/secret-rotation.md) |
| Look up `Tamoss` fields | [Tamoss CR Reference](reference/tamoss-cr.md) |
| Look up `StorageBackend` fields | [StorageBackend CR Reference](reference/storagebackend-cr.md) |

## Minimal CR

```yaml
apiVersion: tamoss.livewyer.io/v1alpha1
kind: Tamoss
metadata:
  name: tamoss-kind
  namespace: tams
spec:
  profile: local-kind
```

For non-local profiles, set the public base domain unless every public endpoint
is configured directly:

```yaml
spec:
  profile: multi-server
  publicEndpoint:
    baseDomain: tamoss.example.com
```

The operator derives API, UI, and S3 endpoints from the base domain. Authentik
endpoints are derived only for Authentik-backed profiles.

## Inspect Effective Configuration

The CR stays intentionally small. To see the configuration after profile
defaults and explicit overrides are applied, inspect status:

```bash
kubectl -n tams describe tamoss tamoss-kind
kubectl -n tams get tamoss tamoss-kind -o jsonpath='{.status.resolved}'
kubectl -n tams get storagebackend -o wide
kubectl -n tams get storagebackend archive -o jsonpath='{.status.resolved}'
```

Status shows generated resource names, image references, endpoints, and Secret
names. It never includes token, password, access key, or private key values.
