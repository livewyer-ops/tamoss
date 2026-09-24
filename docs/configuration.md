# Configuration

A `Tamoss` custom resource selects its release. Omitted site settings inherit
installation defaults; remaining settings use the selected profile's defaults.
Explicit fields in the resource take precedence.
For existing clusters, make durable changes in the generated environment
overlay under `deploy/environments/<name>` and reapply the task workflow.

Use this page as the routing point for configuration work. Field-level details
belong in the references; the canonical CRD schemas are under
`operator/config/crd/bases/`.

## Common Paths

| Need | Use |
| --- | --- |
| Set shared installation defaults | [Runtime Configuration](reference/runtime-configuration.md#installation-defaults) |
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

<a id="minimal-cr"></a>

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

Shared settings go in this file. The platform's `platform-values.yaml` configures
the services it refers to, including the Authentik ingress and TLS issuer.
Keep the Authentik host aligned with the shared domain and `clusterIssuer`
aligned with the platform's `tls.issuerName`.

For the example above, API, UI and S3 use `api.media.team.example.com`,
`app.media.team.example.com` and `s3.media.team.example.com`. Managed authentication
uses `auth.example.com`. Configure DNS for these names before applying the instance;
a wildcard TLS certificate for `*.example.com` does not cover the deeper names.
The operator derives separate TLS Secret names for each instance.

Apply the environment's operator overlay after changing shared settings.
Changes affect existing instances that inherit those fields, independently of
their selected release. See the [installation settings reference](reference/runtime-configuration.md#installation-defaults)
for supported fields and the [upgrade guide](operations/upgrades.md#installation-defaults)
for applying changes to existing installations.

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

To see the effective configuration, inspect status or use `task env:summary`.
For the example instance above:

```bash
kubectl -n team describe tamoss media
kubectl -n team get tamoss media -o jsonpath='{.status.resolved}{"\n"}{.status.endpoints}{"\n"}'
kubectl -n team get storagebackend -o wide
```

Status shows generated resource names, image references, endpoints, and Secret
names. It never includes token, password, access key, or private key values.

## Operator release catalogue

`TAMOSS_RELEASE_CATALOGUE` names the JSON catalogue file mounted into the operator.
Published installations mount an immutable ConfigMap generated from the release
records. The operator loads it at startup and rejects a missing or invalid file.
Select an instance release with `spec.version`; use `spec.images.tamsin` for an
instance-specific ingest image override.
