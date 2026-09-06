# Backup and Restore

TAMOSS stores durable data in PostgreSQL and object storage. Backup ownership
depends on the selected providers.

## Scope

TAMOSS owns backup and restore configuration only for PostgreSQL databases
managed through [CNPG](https://cloudnative-pg.io/). External PostgreSQL
services, [RustFS](https://github.com/rustfs/rustfs), and external
S3-compatible object stores remain provider-owned. Configure and test those
backups with the provider's documented tooling.

TAMOSS does not define a `Backup` CRD. Managed database backup configuration
lives under `Tamoss.spec.backends.db.cnpg`, and the operator renders CNPG
resources.

For on-demand hibernation artefacts and recovery through `TamossHibernate` and
`spec.hibernation`, see [Hibernate and Resume](hibernate-resume.md).

## PostgreSQL Backup

For `providedBy: cnpg`, enable `.spec.backends.db.cnpg.backup` and provide a
CNPG six-field `schedule`, a `retentionPolicy`, and an object-store Secret
reference:

```yaml
spec:
  backends:
    db:
      providedBy: cnpg
      cnpg:
        backup:
          enabled: true
          schedule: "0 0 2 * * *"
          retentionPolicy: "30d"
          objectStore:
            endpointURL: "https://s3.example.com"
            bucket: "tamoss-postgres-backups"
            existingSecret: "tamoss-postgres-backup-creds"
```

The referenced Secret must exist in the same namespace and contain
`AWS_ACCESS_KEY_ID` and `AWS_SECRET_ACCESS_KEY`. The operator renders CNPG
`Cluster.spec.backup` and a `postgresql.cnpg.io/v1.ScheduledBackup`.

`Tamoss.status.conditions[BackupPolicyReady]` and
`Tamoss.status.backupPolicy` report whether the managed policy is configured
and whether CNPG has reported usable backup state:

```bash
kubectl -n tams get tamoss tamoss-multi-server -o jsonpath='{.status.backupPolicy}'
```

Use this status as a readiness signal, not as proof that restore works. Restore
testing remains a separate operational control.

## Restore Into a New Instance

Prefer restoring into a new namespace or a new `Tamoss` instance first. This is
the supported low-risk path because it lets operators verify recovery before
traffic is moved.

1. Confirm the source instance has `BackupPolicyReady=True` with
   `Reason=BackupPolicyHealthy`.
2. Create the backup object-store Secret in the target namespace.
3. Apply a new `Tamoss` CR with CNPG restore enabled.
4. Wait for the CNPG `Cluster` to recover, then wait for
   `SchemaMigrated=True`, `BackendsReady=True`, and `Ready=True`.
5. Validate application reads and writes before moving DNS or routing.

Example target CR fragment:

```yaml
spec:
  profile: multi-server
  publicEndpoint:
    baseDomain: restored.tamoss.example.com
  backends:
    db:
      providedBy: cnpg
      cnpg:
        restore:
          enabled: true
          source: tamoss-source-db
          objectStore:
            endpointURL: "https://s3.example.com"
            bucket: "tamoss-postgres-backups"
            existingSecret: "tamoss-postgres-backup-creds"
```

For point-in-time restore, add `targetTime` in RFC3339 format:

```yaml
        restore:
          enabled: true
          source: tamoss-source-db
          targetTime: "2026-05-22T12:00:00Z"
          objectStore:
            endpointURL: "https://s3.example.com"
            bucket: "tamoss-postgres-backups"
            existingSecret: "tamoss-postgres-backup-creds"
```

The operator renders CNPG `Cluster.spec.bootstrap.recovery` and
`Cluster.spec.externalClusters` from this configuration.

## Replace In Place

Replacing an existing managed database in place is destructive and is not the
normal TAMOSS restore path. Prefer a new-instance restore. If an operator
chooses in-place replacement, pause traffic, verify a final backup, export the
current `Tamoss` and `StorageBackend` resources, record the desired recovery
point, and follow CNPG recovery guidance explicitly.

Do not delete object-storage data as part of a database restore unless a
separate storage-provider restore plan requires it.

## Provider-Owned Data

For `providedBy: external`, use the external provider's backup,
point-in-time recovery, credential rotation, and restore process. TAMOSS does
not create or manage those backup resources.

For managed RustFS Operator storage, follow RustFS backup, replication, and
erasure-coding guidance for the selected pool layout. TAMOSS does not manage S3
backup automation in the current operator.

For managed [Authentik](https://goauthentik.io/) Blueprints, back up the
shared platform Authentik
database and the API token Secret needed to reapply managed Blueprints.
