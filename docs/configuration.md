# Configuration

A `Tamoss` custom resource selects its release. Omitted site settings inherit
installation defaults; remaining settings use the selected profile's defaults.
Explicit fields in the resource take precedence.
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
| Register reusable TAMS Flow Profiles | [Manage Flow Profiles](operations/manage-flow-profiles.md) |
| Approve media inputs for `IngestRun` resources | [IngestRun CR Reference](reference/ingestrun-cr.md) |
| Override runtime workload, image, and environment settings | [Runtime Configuration](reference/runtime-configuration.md) |
| Rotate or mount sensitive values | [Secret Rotation](operations/secret-rotation.md) |
| Look up `Tamoss` fields | [Tamoss CR Reference](reference/tamoss-cr.md) |
| Look up `StorageBackend` fields | [StorageBackend CR Reference](reference/storagebackend-cr.md) |
| Look up `FlowProfile` fields | [FlowProfile CR Reference](reference/flowprofile-cr.md) |
| Look up `IngestRun` fields | [IngestRun CR Reference](reference/ingestrun-cr.md) |

## Minimal resource

With installation defaults configured, an instance needs only its identity and
release:

```yaml
apiVersion: tamoss.livewyer.io/v1alpha1
kind: Tamoss
metadata:
  name: media
  namespace: team
spec:
  version: <release>
```

Replace `<release>` with a supported exact release. The operator calculates
inherited settings without writing them into the resource. Kubernetes may still
supply field defaults declared in the CRD.

## Installation defaults

`task env:init` creates `operator/defaults.yaml` beside the operator Kustomization:

```yaml
profile: single-server
baseDomain: example.com
clusterIssuer: tamoss-public
consoleEnabled: true
```

Kustomize packages this file in an immutable ConfigMap and mounts it at the path
named by `TAMOSS_INSTANCE_DEFAULTS`. Changing the file changes the ConfigMap name
and rolls the operator when the overlay is applied. The operator reads it once
at startup. Shared changes apply to existing instances that inherit those fields.

| Setting | Behaviour when the instance omits its corresponding field |
| --- | --- |
| `profile` | Selects the deployment profile. |
| `baseDomain` | Derives `<name>.<namespace>.<baseDomain>` for the instance. |
| `ingressClassName` | Selects the ingress class. |
| `clusterIssuer` | Supplies the cert-manager annotation when ingress annotations are omitted. |
| `consoleEnabled` | Enables or disables Console. An explicit `false` takes precedence. |
| `authentik.platformNamespace` | Selects the shared Authentik namespace. |
| `authentik.issuerURL` | Selects the shared public Authentik base URL; otherwise `https://auth.<baseDomain>`. |
| `authentik.internalURL` | Overrides the shared internal Authentik base URL. |
| `authentik.apiTokenSecretRef` | Supplies the token Secret `name` and `key` in the Authentik namespace. |

Custom Authentik namespaces must also be allowed by the operator's
`TAMOSS_AUTHENTIK_PLATFORM_NAMESPACES` setting and included in `WATCH_NAMESPACES`
when its watch scope is restricted.

For the example above, API, UI and S3 use `api.media.team.example.com`,
`app.media.team.example.com` and `s3.media.team.example.com`. Managed authentication
uses `auth.example.com`. Configure DNS for these names before applying the instance;
a wildcard TLS certificate for `*.example.com` does not cover the deeper names.
The operator derives separate TLS Secret names for each instance.

Release images and schema targets always come from `spec.version` and its image
overrides. Installation defaults cannot set them. An absent or empty defaults
file leaves explicitly configured instances supported; a version-only resource
reports `InstallationDefaultsRequired`. Invalid configured defaults report
`InvalidInstallationDefaults` and leave existing workloads running.

To override the inherited domain, set `spec.publicEndpoint.baseDomain`. When the
UI uses a non-standard public port, set its exact origin in
`spec.publicEndpoint.uiURL`. Remove an override to resume inheritance. Explicit
external database, storage and authentication settings retain their ownership.

## Browser Origin Checklist

Browser clients need CORS agreement in two places, and the two are owned by
different layers:

1. `spec.api.cors.allowedOrigins` on the `Tamoss` CR for API calls.
2. The bucket CORS rules at the S3 provider for presigned object URLs. For
   external buckets (for example Backblaze B2) these are evaluated per bucket
   and stay provider-owned; the operator does not write them.

After changing either side, the external S3 CORS diagnostic verifies both the
bucket URL preflight and the configured origins. See
[Troubleshooting](operations/troubleshooting.md#s3-and-storagebackend) for the
diagnostic conditions and manual preflight checks.

## OAuth Issuer Details for Clients

With the managed Authentik provider, the issuer URL is per application, not
the Authentik root: `https://<auth-host>/application/o/<application-slug>/`.
OpenID discovery lives beneath it at `.well-known/openid-configuration`. The
client id and secret are held in the instance's generated OAuth Secret;
`task env:summary` prints the resolved values.

## Inspect Effective Configuration

The CR stays intentionally small. To see the configuration after installation and profile
defaults and explicit overrides are applied, inspect status:

```bash
kubectl -n tams describe tamoss tamoss-kind
kubectl -n tams get tamoss tamoss-kind -o jsonpath='{.status.resolved}'
kubectl -n tams get storagebackend -o wide
kubectl -n tams get storagebackend archive -o jsonpath='{.status.resolved}'
```

Status shows generated resource names, image references, endpoints, and Secret
names. It never includes token, password, access key, or private key values.

## Operator release catalogue

`TAMOSS_RELEASE_CATALOGUE` names the JSON catalogue file mounted into the operator.
Published installations mount an immutable ConfigMap generated from the release
records. The operator loads it at startup and rejects a missing or invalid file.
Select an instance release with `spec.version`; use `spec.images.tamsin` for an
instance-specific ingest image override.
