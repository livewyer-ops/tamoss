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

```bash
export KUBECONFIG=/path/to/kubeconfig

task env:init NAME=my-prod PROFILE=multi-server DOMAIN=tamoss.example.com
$EDITOR deploy/environments/my-prod/platform-values.yaml
$EDITOR deploy/environments/my-prod/tamoss-patch.yaml
task env:apply ENV=my-prod KUBECONFIG="$KUBECONFIG"
task env:wait ENV=my-prod KUBECONFIG="$KUBECONFIG"
task env:summary ENV=my-prod KUBECONFIG="$KUBECONFIG"
```

Work through the [Key Settings](#key-settings) while editing the two
generated files, before `task env:apply`.

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
email and public hostnames before applying, and keep the OAuth issuer URL on
its public hostname so browser logins and API token validation agree.

## Validate

```bash
task e2e:deployed PROFILE=multi-server KUBECONFIG="$KUBECONFIG"
```

## Operate

Use the multi-server profile as the baseline for production choices:

- Keep API, UI, worker, PostgreSQL, and S3 resource requests explicit.
- Review the profile defaults for pod security contexts, resource requests,
  PodDisruptionBudgets, pod anti-affinity, and NetworkPolicies before applying
  tenant-specific overrides.
- Use public DNS names and trusted TLS certificates. The default remote platform
  values create `ClusterIssuer/tamoss-public` from `tls.mode: public`; set the
  ACME email before applying.
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
