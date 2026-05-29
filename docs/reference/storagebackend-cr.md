# StorageBackend CR Reference

`StorageBackend` declares an additional BBC TAMS storage backend for one
`Tamoss` instance.

Group: `tamoss.livewyer.io`

Version: `v1alpha1`

Kind: `StorageBackend`

Scope: `Namespaced`

Short name: `sb`

## Minimal External S3 Example

```yaml
apiVersion: tamoss.livewyer.io/v1alpha1
kind: StorageBackend
metadata:
  name: archive
  namespace: tams
spec:
  tamossRef:
    name: tamoss-kind
  provider: external-s3
  region: eu-west-2
  bucketName: archive
  endpoint:
    default:
      url: https://s3.example.com
    public:
      url: https://s3.example.com
  credentials:
    existingSecret: archive-s3-creds
    secretKeys:
      accessKey: accessKeyID
      secretKey: secretAccessKey
```

## Spec Fields

| Field | Purpose |
| --- | --- |
| `.spec.id` | Optional BBC TAMS storage backend UUID. Empty derives a deterministic UUID from namespace/name. |
| `.spec.tamossRef.name` | Target `Tamoss` instance in the same namespace. |
| `.spec.provider` | `rustfs` or `external-s3`. |
| `.spec.defaultStorage` | Whether this backend should be registered as the default backend. |
| `.spec.label` | Public backend label. Defaults to `tamoss.<region>:s3:<bucket>`. |
| `.spec.region` | S3 region value. Defaults to `us-east-1`. |
| `.spec.storeProduct` | TAMS store product. Defaults to `s3`. |
| `.spec.storeType` | TAMS store type. Defaults to `http_object_store`. |
| `.spec.bucketName` | Bucket name. |
| `.spec.endpoint.default.url` | Internal endpoint used by API and worker. |
| `.spec.endpoint.public.url` | Browser-facing endpoint used in object and presigned URLs. Defaults to the default endpoint. |
| `.spec.credentials.existingSecret` | Secret containing access and secret keys. |
| `.spec.credentials.secretKeys.accessKey` | Secret key name for the access key. |
| `.spec.credentials.secretKeys.secretKey` | Secret key name for the secret key. |

## Immutable and Required Fields

The API server rejects updates that change `.spec.id`, `.spec.provider`,
`.spec.bucketName`, or `.spec.tamossRef.name` after creation. These fields map
to durable database registrations and bucket identity.

`.spec.tamossRef.name` is required. For `provider: external-s3`,
`.spec.endpoint.default.url` is also required.

## Status Fields

| Field | Purpose |
| --- | --- |
| `.status.phase` | `Pending`, `Progressing`, `Ready`, or `Degraded`. |
| `.status.backendID` | Registered TAMS storage backend UUID. |
| `.status.bucketName` | Bucket name observed by the operator. |
| `.status.resolved.backendID` | Effective backend UUID after defaults. |
| `.status.resolved.bucketName` | Effective bucket name after defaults. |
| `.status.resolved.provider` | Effective provider, currently `rustfs` or `external-s3`. |
| `.status.resolved.endpointURL` | Internal endpoint used by API and worker. |
| `.status.resolved.publicEndpointURL` | Browser-facing endpoint used in object and presigned URLs. |
| `.status.resolved.credentialsSecret` | Secret name containing backend credentials. Secret values and key names are not exposed. |
| `.status.conditions` | `Ready`, `BucketReady`, and `DatabaseReady` condition details. |

## Provider Behavior

- `rustfs`: the operator can create and delete managed RustFS bucket resources.
- `external-s3`: the operator registers metadata and runtime credentials only.
  Bucket lifecycle, CORS, IAM, retention, replication, backups, and deletion
  remain provider-owned.

See [Storage Backends](../concepts/storage-backends.md).
Use [CRD Versioning](crd-versioning.md) for API stability and migration policy.
