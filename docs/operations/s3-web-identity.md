# Configure S3 web identity

Use this procedure to run the operator-managed API and worker against an
existing default S3 media bucket using IAM roles for service accounts (IRSA).
Check the [release prerequisites](../../CHANGELOG.md#upgrade-notes) before
changing an existing installation.

## Configure the identity and bucket

Create the bucket and configure its browser CORS policy outside TAMOSS. Create
an IAM role whose trust policy binds the cluster's OIDC issuer, the intended
namespace and ServiceAccount, with audience `sts.amazonaws.com`. Grant the
object operations needed for upload, download, metadata reads, copies and
deletion, plus any permissions required by the bucket's encryption policy.
Follow [AWS's IRSA configuration procedure](https://docs.aws.amazon.com/eks/latest/userguide/associate-service-account-role.html).

The cluster must provide native IRSA token injection. Allow API and worker
egress to the configured STS and S3 endpoints through the installation's
existing network policy. The operator does not create IAM roles or call STS.

## Configure the instance and backend

Merge this fragment into the existing Tamoss manifest, using your bucket,
region, endpoint and role. Keep the instance's selected `spec.version`.

```yaml
spec:
  serviceAccount:
    create: true
    automount: false
    annotations:
      eks.amazonaws.com/role-arn: arn:aws:iam::123456789012:role/tamoss-media
      eks.amazonaws.com/sts-regional-endpoints: "true"
  backends:
    s3:
      providedBy: external
      external:
        credentialSource: web-identity
        endpoint:
          default:
            url: https://s3.eu-west-1.amazonaws.com
        region: eu-west-1
        bucket: media
```

Remove the external S3 `auth.existingSecret` reference. Remove static storage
credentials and AWS credential-profile overrides from API/worker `env`,
`envFrom` and mounted configuration. SDK credential providers retain their
native precedence, so an independently injected static credential can otherwise
prevent web identity from being used.

Declare the companion StorageBackend in the same namespace:

```yaml
apiVersion: tamoss.livewyer.io/v1alpha1
kind: StorageBackend
metadata:
  name: default-media
spec:
  id: f1ab5b54-9703-42ed-b181-11ba1c794a7f
  tamossRef:
    name: tamoss
  provider: external-s3
  usage: media
  defaultStorage: true
  credentialSource: web-identity
  region: eu-west-1
  bucketName: media
  endpoint:
    default:
      url: https://s3.eu-west-1.amazonaws.com
```

For an existing backend, edit its current resource in place and remove its
`credentials.existingSecret` reference. Its ID, bucket and provider are immutable;
an existing backend with a different ID does not support this configuration.
The bucket, region and both endpoints must match the Tamoss configuration.
An omitted public endpoint uses the default endpoint.

Apply both manifests. Reconciliation waits for matching configuration. Confirm
both resources' observed generations are current and wait for the API and worker
rollouts. The shared runtime credential file retains additional backend
credentials but excludes the default web identity backend.

Changes to annotations on an operator-created ServiceAccount trigger API and
worker rollout. With `serviceAccount.create: false`, select an existing annotated
account using `serviceAccount.name`; restart the API and worker Deployments
after changing that account's identity configuration.

## Verify media operations

Test upload and download through returned presigned URLs, Segment registration,
worker copies and deletion. Confirm both API and worker Pods have the intended
`AWS_ROLE_ARN` and a readable projected token file. Do not print token contents,
temporary credentials or signed URLs in diagnostic reports.

Readiness confirms configuration and registration; it does not establish AWS
trust or bucket permissions. RustFS STS integration tests exercise SDK and
storage behaviour locally. AWS IAM and EKS permissions require a live AWS test.

## Restore Secret credentials

Retain the original user-owned credential Secret during migration. To roll
back, set both resources' `credentialSource` to `secret`, restore their original
Secret references and key selectors, apply both manifests and wait for the API
and worker rollouts. Verify media operations before downgrading the operator or
runtime. The operator does not delete the retained credential Secret.

Additional media backends, managed RustFS credentials and database hibernation
continue to require their existing Secret configuration. EKS Pod Identity is
not supported by this web identity option.
