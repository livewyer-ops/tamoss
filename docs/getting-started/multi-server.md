# Multi Server

`multi-server` is the production reference profile. Use it for durable,
multi-node self-managed Kubernetes installs.

## Requirements

Before applying the profile, confirm:

- The cluster has enough schedulable CPU, memory, and storage for replicated
  API, worker, UI, PostgreSQL, and S3 workloads.
- A default StorageClass exists, or the `Tamoss` CR names storage classes for
  [CNPG](https://cloudnative-pg.io/) and
  [RustFS](https://github.com/rustfs/rustfs) Operator volumes.
- Public DNS exists for API, UI, S3, and
  [Authentik](https://goauthentik.io/) hostnames.
- TLS issuance is planned through [cert-manager](https://cert-manager.io/) or
  existing TLS Secrets.
- Secret management is in place for database, S3, OAuth, Authentik, and API
  token material.
- The CNI enforces Kubernetes NetworkPolicy if you rely on the profile's
  default traffic restrictions.
- PostgreSQL and object-storage backup/restore ownership is decided before
  users write durable data.

## Install

For local validation on [Kind](https://kind.sigs.k8s.io/):

```bash
task kind:up PROFILE=multi-server
```

This validates the `multi-server` profile shape on Kind; it is not the
existing-cluster install path. The local harness uses
`deploy/kind-multi-server.yaml` to create one control-plane node and three worker
nodes, so the operator's multi-server scheduling defaults are exercised before
the deployed e2e checks run.

For an existing cluster:

Choose the product release and export it as `TAMOSS_VERSION`. The generated
environment uses it for the operator install reference and initial instance.

```bash
export KUBECONFIG=/path/to/kubeconfig

task env:init TAMOSS_VERSION="$TAMOSS_VERSION" NAME=my-prod PROFILE=multi-server DOMAIN=tamoss.example.com
$EDITOR deploy/environments/my-prod/platform-values.yaml
$EDITOR deploy/environments/my-prod/operator/defaults.yaml
task env:summary ENV=my-prod KUBECONFIG="$KUBECONFIG"
```

Work through the [Key Settings](#key-settings), then apply the platform,
operator and instance with the native commands in the
[install guide](../operations/install.md#existing-cluster). Use
`task env:summary` to inspect status if useful.

The platform layer installs the components enabled in
`deploy/environments/my-prod/platform-values.yaml`. The TAMOSS operator
reconciles the instance resources selected by the `Tamoss` CR; it does not
install platform operators from inside a `Tamoss` reconcile.

## Key Settings

### High availability

The profile defaults to two replicas each of API, worker, and UI with
PodDisruptionBudgets, pod anti-affinity, and NetworkPolicies enabled, and
three CNPG PostgreSQL instances with a 100 GiB volume each. Review these
defaults before overriding them. Lowering replicas or removing
PodDisruptionBudgets gives up the profile's zero-downtime upgrade behaviour.

### Backups

Decide backup ownership before accepting durable data. Scheduled CNPG
backups and restore procedures are covered in
[Backup and Restore](../operations/backup-restore.md); object storage
replication stays with the S3 provider. For planned shutdowns of whole
instances, see [Hibernate and Resume](../operations/hibernate-resume.md).

### Identity

The profile selects the managed Authentik stack by default. Set the ACME
email and public hostnames before applying. The Authentik ingress in
`platform-values.yaml` must match the shared domain or explicit Authentik URL
in `operator/defaults.yaml`. See
[Configuration](../configuration.md#installation-defaults) for instance and
shared hostname rules.

## Validate

```bash
task e2e:deployed PROFILE=multi-server KUBECONFIG="$KUBECONFIG"
```

This command uses the checked-in Kind target. For a remote cluster, copy
[`tests/targets/remote.env.example`](../../tests/targets/remote.env.example) to
`deploy/environments/my-prod/target.env`. Use `task env:summary` and the
instance's `status.endpoints` and `status.resolved.generatedSecrets` for the
effective URLs and Secret names. Set `TEST_TAMOSS_NAMESPACE=tams`,
`TEST_TAMOSS_CR_NAME=tamoss-multi-server` and the resolved token Secret name in
`TEST_TAMOSS_TOKEN_SECRET`. Retrieve credentials with
`task env:credentials ENV=my-prod INSTANCE=tamoss-multi-server KUBECONFIG="$KUBECONFIG"`
and supply browser login credentials through
`TEST_TAMOSS_AUTH_USER` and `TEST_TAMOSS_AUTH_PASSWORD` or the target's password
Secret reference, then run:

```bash
task e2e:deployed PROFILE=multi-server KUBECONFIG="$KUBECONFIG" \
  TARGET_ENV=deploy/environments/my-prod/target.env
```

The media checks need existing media. A fresh remote installation is empty;
configure its ingest source policy and ingest test media before running those
checks. See [Manage Ingest Runs](../operations/manage-ingest-runs.md).

## Operate

Use the multi-server profile as the baseline for production choices:

- Review the profile defaults for pod security contexts, resource requests,
  PodDisruptionBudgets, pod anti-affinity, and NetworkPolicies before applying
  tenant-specific overrides.
- Console is enabled by the generated installation defaults. Its default
  Kubernetes API egress is port-scoped. Set
  `spec.networkPolicy.kubernetesAPIIPBlocks` when destination restrictions are
  required; see [Runtime Configuration](../reference/runtime-configuration.md).
- Use public DNS names and trusted TLS certificates. The default remote platform
  values create `ClusterIssuer/tamoss-public` from `tls.mode: public`; set the
  ACME email before applying and keep the installation's `clusterIssuer`
  aligned with the platform's `tls.issuerName`.
- Keep internal service URLs separate from public OAuth issuer and public S3
  URLs.
- Confirm API CORS and browser-facing S3 CORS permit every browser origin that
  will call the API or dereference presigned object URLs.
- Test restore for PostgreSQL and object storage before accepting production
  data.

See also:

- [Provider Ownership](../concepts/provider-ownership.md)
- [Backup and Restore](../operations/backup-restore.md)
- [Upgrades](../operations/upgrades.md)
