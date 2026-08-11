# Storage Backends

TAMOSS exposes BBC TAMS storage backends through database metadata and runtime
credentials. In Kubernetes installs, `StorageBackend` is the storage
provisioning and database-registration method. The operator emits the default
`StorageBackend` for a `Tamoss` instance, and additional backends are declared
with more `StorageBackend` resources in the same namespace.

## Default Backend

The default backend is selected by:

```yaml
spec:
  backends:
    s3:
      providedBy: rustfs-operator
      tags:
        access: [programme, archive]
        tier: hot
```

The built-in profiles use [CNPG](https://cloudnative-pg.io/) and
[RustFS](https://github.com/rustfs/rustfs) Operator by default. Use
`providedBy: external` when the default backend is an external S3-compatible
bucket.

## Additional Backend

Use `StorageBackend` to add another TAMS storage backend to one `Tamoss`
instance:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: archive-s3-creds
  namespace: tams
stringData:
  accessKeyID: example-access-key
  secretAccessKey: example-secret-key
---
apiVersion: tamoss.livewyer.io/v1alpha1
kind: StorageBackend
metadata:
  name: archive
  namespace: tams
spec:
  tamossRef:
    name: tamoss-kind
  provider: external-s3
  tags:
    access: [programme, archive]
    tier: cold
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

Storage backend tags are freeform TAMS metadata whose values can be either a
single string or an array of strings. Both forms are preserved when exposed
read-only by `/service/storage-backends` and can be used by clients to filter
backend listings, object download URLs, and storage allocation choices. Keep
authorisation decisions in the API or storage provider; tags describe policy
inputs and are not credentials.

## Runtime Credentials

Storage backend metadata is stored in PostgreSQL. Access keys are not stored in
the database or API responses.

For Kubernetes installs:

1. `StorageBackend.spec.credentials` references a Secret in the namespace.
2. The operator renders one derived runtime credentials Secret per `Tamoss`
   instance.
3. API and worker pods mount that Secret as a credentials file.
4. The API reloads the file when Kubernetes refreshes the mounted Secret.

Runtime credential files take precedence over credentials persisted in a storage
backend row. The object-storage client cache is keyed by a credential
fingerprint, so a Secret rotation causes subsequent S3 operations to build a new
client and close the stale cached client when the underlying SDK supports it.

For non-Kubernetes runtime experiments, provide the same credentials file
through your local secret-management mechanism. If you need the Python runtime
to seed a default storage backend row for a native experiment, opt in
explicitly with `TAMOSS_STORAGE_BACKEND_REGISTRATION_ENABLED=true`; Kubernetes
operator-managed API and worker pods set this to `false` and consume
`StorageBackend`-registered rows.

## Controlled Storage Allocation

Controlled storage allocation has an explicit lifecycle. A `POST
/flows/{flowId}/storage` response reserves object IDs and returns PUT upload
requests, but those objects are not referenceable by Flow Segments until the
client finalises the controlled object with `POST /objects/{objectId}/instances`
and the matching `storage_id`.

Finalisation verifies the object exists in the configured object store and
records available storage metadata such as content length, content type, ETag,
checksum, and observation time.

Allocation requests are bounded by `TAMOSS_STORAGE_ALLOCATION_MAX_OBJECTS`
and object IDs by `TAMOSS_STORAGE_OBJECT_ID_MAX_LENGTH`. Clients must upload,
finalise, and then register segments; allocated-only controlled objects are not
referenceable by Flow Segments.

## External S3 Browser Access

Browser clients access external object storage directly through presigned URLs.
Ingest uses `put_url`; playback and downloads use `get_urls`. Curl and
server-side tests can succeed while browsers fail if the external bucket CORS
policy does not allow the browser origin, method, or requested headers.

External S3 buckets must allow every browser origin that will dereference
presigned URLs. This can include the TAMOSS UI origin and external tools such as
review, playback, or ingest clients. At minimum, configure the following:

- Origins: browser origins such as `https://app.tamoss.example.com` and
  `https://tool.example.com`.
- Upload methods: `PUT`.
- Read methods: `GET` and `HEAD`.
- Upload headers: at least `content-type`; `*` allows all headers where
  acceptable.
- Read/playback headers: `range` is commonly required by media clients. Some
  XHR-based clients also preflight `authorization`, `x-requested-with`,
  `cache-control`, or `pragma` even when the presigned URL itself carries the
  storage credentials.

TAMOSS API CORS and bucket CORS are separate. Configure API CORS with
`.spec.api.cors.allowedOrigins` and optional
`.spec.api.cors.allowedOriginRegexes` when browser tools call the TAMOSS API.
TAMOSS does not configure bucket CORS for `external-s3` backends.

## Managed RustFS Browser Access

For managed RustFS backends the operator configures bucket CORS automatically,
including the UI web host from `.spec.ingress.ui.web.host` (`https` when
`.spec.ingress.tls` is set, otherwise `http`) and exact origins from
`.spec.api.cors.allowedOrigins`. When no exact origins are configured, the
operator falls back to a wildcard (`*`) bucket CORS origin.

Regex origins from `.spec.api.cors.allowedOriginRegexes` are supported by the
API and at the [Traefik](https://traefik.io/) S3 ingress layer. They are not
written to RustFS bucket
CORS because S3-compatible bucket CORS rules are exact-origin based. Use an
`external-s3` backend and configure the provider's bucket CORS policy directly
when you need provider-specific CORS behaviour.

## Deletion

Deleting a `StorageBackend` requires the confirmation annotation. Managed RustFS
bucket resources are created, configured for CORS, and cleaned up by the
operator through its native S3-compatible client. `external-s3` deletion removes
only TAMOSS database registration and operator state; the provider bucket
remains untouched.
