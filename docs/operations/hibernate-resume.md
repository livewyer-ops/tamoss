# Hibernate and Resume

Hibernate quiesces TAMOSS, captures the managed database into an external
S3-compatible bucket, records a portable manifest, and removes the source CNPG
cluster and its managed storage. Resume reads that manifest, restores the exact
CNPG backup into a target `Tamoss`, and waits for all TAMOSS services to become
ready.

This workflow moves managed PostgreSQL state, not the Kubernetes cluster
itself. It does not back up TAMS media objects, external PostgreSQL services,
managed RustFS data, or Authentik state. Source and target instances must use
external S3 media storage that remains accessible at the resumed location.

## Requirements

- `Tamoss.spec.backends.db.providedBy: cnpg`.
- `Tamoss.spec.backends.s3.providedBy: external` on both source and target.
- The source `Tamoss` reports `SchemaMigrated=True` with a schema revision
  supported by the running operator.
- The CNPG operator installed, including its `Backup` CRD; Hibernate creates
  `postgresql.cnpg.io` `Backup` resources.
- A `StorageBackend` in the same namespace with `spec.usage: hibernate`.
- `StorageBackend.spec.provider: external-s3`.
- A credentials Secret in the same namespace as the `StorageBackend`. The
  credentials need object write access for Hibernate, read access for Resume,
  and list plus delete access when a `DeleteAfterResume` or `TTL` retention
  mode is configured.
- The target Resume namespace must have access to the same hibernation bucket
  and manifest object, through a hibernate `StorageBackend` whose (required)
  `spec.tamossRef.name` points at the target `Tamoss` and which reports
  `Ready`.

Hibernate destinations are not registered as TAMS media storage backends and
are not exposed to API or worker runtime credentials.

`TamossHibernate` and `TamossResume` are covered by the same deletion
protection webhooks as `Tamoss` and `StorageBackend`: deletes are rejected
until the `confirmation.tamoss.livewyer.io/deletion: "true"` annotation is
present. Add the annotation when you intend to delete, not at creation time,
so the protection stays armed. See
[Deletion Protection](deletion-protection.md).

Operations created before their dependencies are ready are safe: the
controllers record a waiting phase such as `PreparingTarget` or
`ResolvingSource` and poll until the referenced `Tamoss`, `StorageBackend`,
and database cluster exist and become ready.

## Hibernate

Create a hibernate destination:

```yaml
apiVersion: tamoss.livewyer.io/v1alpha1
kind: StorageBackend
metadata:
  name: tamoss-hibernate
  namespace: tams
spec:
  tamossRef:
    name: tamoss-cnpg
  provider: external-s3
  usage: hibernate
  region: eu-west-2
  bucketName: tamoss-hibernations
  endpoint:
    default:
      url: https://s3.example.com
  credentials:
    existingSecret: tamoss-hibernate-s3
    secretKeys:
      accessKey: AWS_ACCESS_KEY_ID
      secretKey: AWS_SECRET_ACCESS_KEY
  hibernate:
    retention:
      mode: Retain
```

Wait for the destination:

```bash
kubectl -n tams wait storagebackend/tamoss-hibernate --for=condition=Ready --timeout=5m
```

Create a hibernation request:

```yaml
apiVersion: tamoss.livewyer.io/v1alpha1
kind: TamossHibernate
metadata:
  name: snapshot-20260707
  namespace: tams
spec:
  tamossRef:
    name: tamoss-cnpg
  destination:
    storageBackendRef:
      name: tamoss-hibernate
    prefix: hibernations/tamoss-cnpg
```

The operator suspends the managed CNPG `ScheduledBackup`, points the managed
CNPG `Cluster.spec.backup` at the hibernation destination, removes managed
HorizontalPodAutoscalers, and scales TAMOSS API, worker, and UI Deployments to
zero. It waits for both Deployment status and matching live Pods to quiesce,
launches a CNPG `Backup`, uploads the manifest, and then deletes the source
CNPG cluster. The operation becomes `Completed` only after source database
deprovisioning has been observed.

The `Tamoss` CR and non-database Kubernetes resources remain in place and
normal reconciliation stays gated. The underlying Kubernetes nodes or cluster
are not shut down by this operation.

Check the artifact:

```bash
kubectl -n tams get tamosshibernate snapshot-20260707 \
  -o jsonpath='{.status.phase}{" "}{.status.artifact.manifestURI}{"\n"}'
```

The parent `Tamoss` lifecycle moves to `Hibernated`, and normal reconciliation
stays gated until a Resume operation completes.

## Resume

Resume can target a different cluster or namespace as long as the target
namespace has a `Tamoss` CR and a hibernate `StorageBackend` that points at the
same external bucket.

For a new target instance, create the target `Tamoss` with `spec.paused: true`
first. This prevents normal reconciliation from creating a fresh CNPG cluster
before Resume has created the recovery cluster. When Resume reaches
`StartingServices`, set `spec.paused: false`. Resume retains its lifecycle claim
while normal reconciliation starts the services, and completes only after the
target reports a current-generation `Ready=True`.

The target CNPG cluster must not already exist unless it was created by the
same `TamossResume` operation. The operator fails the Resume instead of
rewiring an existing database cluster.

Resume from a manifest key:

```yaml
apiVersion: tamoss.livewyer.io/v1alpha1
kind: TamossResume
metadata:
  name: resume-20260707
  namespace: restored
spec:
  tamossRef:
    name: tamoss-cnpg-restored
  source:
    artifact:
      storageBackendRef:
        name: tamoss-hibernate
      manifestKey: hibernations/tamoss-cnpg/snapshot-20260707/manifest.json
      checksum: sha256:<checksum-from-tamosshibernate-status>
```

If the completed `TamossHibernate` is in the same namespace as a clean target
`Tamoss`, Resume can refer to it directly:

```yaml
apiVersion: tamoss.livewyer.io/v1alpha1
kind: TamossResume
metadata:
  name: resume-20260707
  namespace: tams
spec:
  tamossRef:
    name: tamoss-cnpg-restored
  source:
    hibernationRef:
      name: snapshot-20260707
```

Watch Resume status:

```bash
kubectl -n restored get tamossresume resume-20260707 \
  -o jsonpath='{.status.phase}{" "}{.status.reason}{"\n"}'
```

Resume restores the exact `backupID` committed in the manifest. When CNPG
reports the recovery cluster ready, the operation enters `StartingServices`.
After the target is unpaused and all components report ready, the operator
marks `TamossResume` as `Completed` and clears the active lifecycle operation.

Before creating the recovery cluster, Resume validates the manifest kind,
source schema revision, and TAMS API compatibility against the running
operator. Missing or unsupported compatibility metadata fails the operation
without changing the target database infrastructure.

## Artifact Retention

Hibernate writes one artifact prefix per operation:

```text
s3://<bucket>/<prefix>/<tamosshibernate-name>/manifest.json
s3://<bucket>/<prefix>/<tamosshibernate-name>/cnpg/...
```

The manifest SHA-256 checksum is recorded in `TamossHibernate.status`. Resume
validates it before creating the recovery cluster. A direct artifact source
must supply that trusted checksum explicitly; the object store's copy of the
manifest is not its own integrity anchor. CNPG/Barman remains responsible for
the database backup object set below the `cnpg/` prefix.

The hibernate `StorageBackend` referenced by `TamossResume.spec.source`
controls what happens to the source artifact after a successful Resume through
`.spec.hibernate.retention.mode`:

| Mode | Behaviour |
| --- | --- |
| `Retain` | Default. Leave the hibernation artifact in the bucket. |
| `DeleteAfterResume` | Delete the artifact prefix immediately after Resume completes. |
| `TTL` | Delete the artifact prefix after `.spec.hibernate.retention.ttlSecondsAfterResume` seconds. |

Cleanup uses S3-compatible list and delete calls against the artifact prefix.
This keeps the operator contract portable across AWS S3, GCS S3 interop,
Backblaze B2 S3, RustFS, and similar stores without depending on
provider-specific lifecycle rules. Resume remains `Completed` whatever happens
to cleanup. Transient deletion failures (permissions, endpoint problems,
missing credentials) leave `.status.artifact.cleanup` in phase `Pending` with
reason `ArtifactCleanupRetrying` and are retried every minute, with a warning
Event on the first failure. Structural problems (an invalid manifest key, or
the hibernate `StorageBackend` no longer resolving) set phase `Blocked`, which
is terminal: fix the cause and delete the objects manually, or recreate the
`TamossResume` if you need the operator to retry.

Check cleanup state:

```bash
kubectl -n restored get tamossresume resume-20260707 \
  -o jsonpath='{.status.artifact.cleanup.phase}{" "}{.status.artifact.cleanup.reason}{"\n"}'
```

Cleanup uses the credentials Secret configured on the referenced hibernate
`StorageBackend`. For `DeleteAfterResume` or `TTL`, those credentials must be
allowed to list and delete objects under the hibernation prefix. Retained
artifacts are not incremental; each Hibernate operation writes a separate
database backup artifact.

## Failure Handling

Both operations distinguish waiting states from terminal failures:

- Waiting phases (`Pending`, `ResolvingSource`, `PreparingTarget`, `Quiescing`,
  `CapturingDatabase`, `WritingManifest`, `DeprovisioningSource`,
  `RecoveringDatabase`, `StartingServices`) are retried on a poll interval.
  `Quiescing` waits for managed workload replicas and live Pods to reach zero
  before database capture. Transient S3 problems during manifest upload or read
  keep the operation in `WritingManifest` or `ResolvingSource` with the error
  in `.status.message` until they clear.
- `Failed` is terminal. Failed CNPG backups, checksum mismatches, corrupt or
  missing manifests, unsupported drivers, and lifecycle conflicts are not
  retried, and the spec fields of both CRs are immutable. To retry, create a
  new `TamossHibernate` or `TamossResume` with a fresh name.
- Deleting a `TamossHibernate` or `TamossResume` that has not completed marks
  the parent `Tamoss` lifecycle `Failed`; normal reconciliation can then
  continue. Aborting Resume also removes the recovery CNPG cluster created by
  that exact Resume UID before its finalizer clears, so a retry starts cleanly.
- `DeprovisioningSource` is Hibernate's commit point. If deletion is requested
  after the manifest is committed, finalization finishes source database
  removal and leaves the parent `Hibernated`; it does not roll back into a new
  empty database.
- Deleting a `Completed` operation is housekeeping and leaves the parent
  `Tamoss` lifecycle untouched, but a `TamossHibernate` referenced by a future
  `TamossResume.spec.source.hibernationRef` must still exist; keep it, or
  resume from the manifest key instead.

See [Troubleshooting](troubleshooting.md) for general status and Event
inspection commands.

## Field Notes

- `TamossHibernate.spec` (`tamossRef.name`, `destination.storageBackendRef`,
  `destination.prefix`, `driver`) and `TamossResume.spec` (`tamossRef.name`,
  `source.hibernationRef` or `source.artifact`) are immutable after creation.
- `spec.driver` accepts `cnpgPhysical` (default) and `logicalDump`;
  `logicalDump` is reserved and currently fails with `UnsupportedProvider`.
- Progress is reported through `.status.phase`, `.status.reason`,
  `.status.message`, `Ready`/`Progressing`/`Degraded` conditions, and
  `.status.artifact` (driver, manifest key and URI, checksum, CNPG backup
  details, cleanup state).
- The parent `Tamoss` reports `.status.lifecycle` (phase, active operation,
  and last hibernate/resume references) and a `LifecycleReady` condition.

## Current Limits

- `cnpgPhysical` is the implemented Hibernate driver.
- `logicalDump` is reserved for a later PostgreSQL logical dump path.
- Resume creates a managed CNPG recovery cluster; it does not restore into an
  existing unmanaged database.
- Resume refuses to replace an existing cluster that was not created by the
  same Resume operation.
- Artifact activation is not yet coordinated across Kubernetes clusters. Do
  not start concurrent Resume operations from the same retained artifact; use
  external orchestration to enforce a single active target.
- Full Suspend/Resume based only on retained PVCs is a separate lifecycle shape
  and is not implemented here.

See [Backup and Restore](backup-restore.md) for scheduled backup policy and
provider-owned data guidance.
