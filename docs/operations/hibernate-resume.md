# Hibernate and Resume

Hibernation captures the managed TAMOSS database into an external
S3-compatible bucket as a portable, checksummed artifact, quiesces the
workloads, and removes the database compute. Resuming bootstraps a managed
database from such an artifact, either waking the same instance or seeding a
new instance in another namespace or cluster.

The lifecycle is declared on the `Tamoss` spec. The operator materialises a
`TamossHibernate` operation for each hibernation cycle; the operation records
progress, the artifact identity, and its trusted checksum.

This workflow is database-only. It does not copy TAMS media objects or
[Authentik](https://goauthentik.io/) state.

## Supported Backend Combinations

| Database | Media S3 | Hibernate and resume |
| --- | --- | --- |
| Managed (`providedBy: cnpg`) | External | Supported. The workflow on this page. |
| Managed (`cnpg` or `bundled`) | Managed ([RustFS](https://github.com/rustfs/rustfs)) | Not supported. Managed media would be lost. |
| Managed (`providedBy: bundled`) | External | Not supported. Capture requires the [CNPG](https://cloudnative-pg.io/) `Backup` resource. |
| External | External | Not needed. Redeploy against the same database and bucket. |

The artifact captures only the database, so hibernation refuses managed
RustFS media and the operation fails with `UnsupportedProvider`. The media
objects would be removed with the source instance, leaving a resumed database
referencing deleted segments. With an external database there is nothing
managed to capture or deprovision. The database and media both outlive the
instance, so deleting the `Tamoss` and redeploying with the same external
connection details already achieves what hibernate and resume provide.

## Requirements

- `Tamoss.spec.backends.db.providedBy: cnpg` and
  `Tamoss.spec.backends.s3.providedBy: external`.
- The CNPG operator installed, including its `Backup` CRD.
- A `StorageBackend` in the same namespace with `spec.usage: hibernate` and
  `spec.provider: external-s3`, plus a credentials Secret. The credentials
  need object write access to hibernate, read access to resume, and list plus
  delete access for `DeleteAfterResume`/`TTL` retention.
- A resuming namespace needs its own hibernate `StorageBackend` pointing at
  the same bucket, with its (required) `spec.tamossRef.name` set to the
  target instance.

## Hibernate

Declare hibernation on the instance:

```yaml
spec:
  hibernation:
    enabled: true
    destination:
      storageBackendRef:
        name: tamoss-hibernate
      prefix: hibernations/tamoss-cnpg
```

The operator then creates `<name>-hibernation-<cycle>` (a `TamossHibernate`),
which quiesces the API, worker, and UI workloads, suspends routine CNPG
backups, captures the database with a CNPG `Backup`, uploads a checksummed
manifest, and finally removes the source CNPG cluster. Watch progress:

```bash
kubectl -n tams get tamosshibernations
kubectl -n tams get tamoss tamoss-cnpg \
  -o jsonpath='{.status.lifecycle.phase}{" "}{.status.lifecycle.message}{"\n"}'
```

The operation moves through `Quiescing`, `CapturingDatabase`,
`WritingManifest`, and `DeprovisioningSource` before `Completed`. The parent
lifecycle then reports `Hibernated`, and reconciliation stays gated while
`spec.hibernation.enabled` is true. Hibernation is destructive by design.
Once the source cluster is removed, the artifact in the bucket is the
authoritative copy of the database, so keep the default `Retain` retention
until a resume has been verified.

Setting `enabled: false` again wakes the instance. The operator resolves the
most recent hibernation artifact, verifies its checksum, and bootstraps a new
CNPG cluster from it before normal reconciliation restores the workloads.

Creating a `TamossHibernate` directly still works and behaves identically;
the spec toggle is the recommended, GitOps-friendly path. See
`operator/config/samples/tamoss_v1alpha1_tamosshibernate.yaml` for a sample.

## Resume Into a New Instance

Declare the source on the new instance. It is honoured only while the
database cluster does not exist, exactly like CNPG bootstrap:

```yaml
spec:
  hibernation:
    resumeFrom:
      hibernationRef:
        name: tamoss-cnpg-hibernation-1   # same-namespace TamossHibernate
```

Across namespaces or clusters, refer to the artifact identity instead:

```yaml
spec:
  hibernation:
    resumeFrom:
      artifact:
        storageBackendRef:
          name: tamoss-hibernate
        manifestKey: hibernations/tamoss-cnpg/tamoss-cnpg-hibernation-1/manifest.json
        checksum: sha256:…   # from the TamossHibernate status.artifact.checksum
```

The checksum is required for artifact restores. It proves that the manifest
in the bucket is the one the hibernation wrote. The operator
reads and validates the manifest (checksum, schema version, TAMS API
compatibility, completed CNPG backup), persists the resolved source in
`.status.lifecycle.resolvedRestore`, and renders the CNPG recovery bootstrap
from it on every reconcile. The lifecycle reports `Resuming` until the
restored database is ready, then `Running` while the workloads finish
starting up.

If the database cluster already exists, the operator ignores `resumeFrom` and
emits a `ResumeSourceIgnored` warning event. `resumeFrom` never replaces an
existing database.

## Artifact Retention

The hibernate `StorageBackend` controls what happens to the artifact after a
successful resume, through `.spec.hibernate.retention.mode`:

| Mode | Behaviour |
| --- | --- |
| `Retain` | Default. Leave the hibernation artifact in the bucket. |
| `DeleteAfterResume` | Delete the artifact prefix once the restored database is ready. |
| `TTL` | Delete the artifact prefix `.spec.hibernate.retention.ttlSecondsAfterResume` seconds after the restored database became ready. |

The resumed instance applies retention, anchored on database readiness.
Cleanup does not wait for unrelated workload start-up; it proceeds once the
database the artifact carried runs again. The restore completion time is
recorded first and progress is reported in
`.status.lifecycle.resolvedRestore.cleanup`. Transient deletion failures
retry every minute with a warning event. Structural problems, such as an
invalid manifest key or a missing or wrong-typed `StorageBackend`, set the
terminal `Blocked` phase. After that, remove the objects manually. Cleanup
uses plain S3 list and delete calls, so it works identically on AWS S3, B2,
RustFS, and similar stores.

## Failure Handling

- Waiting states (`Pending`, `ResolvingSource`, `PreparingTarget`,
  `Quiescing`, `CapturingDatabase`, `WritingManifest`,
  `DeprovisioningSource`) poll until their dependencies settle. Transient S3
  errors during manifest upload or read stay non-terminal, with the error in
  the status message.
- Retry a `Failed` operation by annotating it with a fresh value:

  ```bash
  kubectl -n tams annotate tamosshibernate tamoss-cnpg-hibernation-1 \
    tamoss.livewyer.io/operation-retry="$(date +%s)" --overwrite
  ```

  Each distinct value re-arms the operation exactly once;
  `.status.acceptedRetry` records the last honoured value. Alternatively,
  toggling `spec.hibernation.enabled` off and on starts a fresh cycle with a
  new artifact.
- While `spec.hibernation.enabled` is true, the instance stays gated even if
  a cycle fails. A failure never implicitly restores a hibernated instance.
  Disabling hibernation mid-capture aborts the materialised operation and
  returns the instance to normal reconciliation.
- A failed `resumeFrom` bootstrap, such as a checksum mismatch, unsupported
  schema, or corrupt manifest, marks the lifecycle `Failed` with the reason.
  Because `resumeFrom` is immutable, recover by recreating the instance with
  a corrected source.

## Field Notes

- The API server enforces these rules: `spec.hibernation.resumeFrom` and its
  nested fields are immutable, `destination` is required while `enabled` is
  true, and `resumeFrom` must set exactly one of `hibernationRef` or
  `artifact`.
- `TamossHibernate.spec` is immutable after creation. Operator-materialised
  operations carry the deletion confirmation annotation so the operator can
  abort its own cycles. Delete protection still applies to user-created
  operations; see [Deletion Protection](deletion-protection.md).
- The parent `Tamoss` reports a `LifecycleReady` condition and
  `.status.lifecycle` with the phase, reason, message, `lastTransitionTime`,
  `hibernationCycle`, the active and last operation references, and
  `resolvedRestore`.
- Keep completed `TamossHibernate` resources while any instance may still
  `resumeFrom` them by reference. Artifact-based restores need only the
  bucket contents and the recorded checksum.

## Current Limits

- `cnpgPhysical` is the implemented hibernation driver. `logicalDump` is
  reserved for a later PostgreSQL logical dump path.
- Provider support is limited to the combinations in
  [Supported Backend Combinations](#supported-backend-combinations).
- `resumeFrom` cannot restore into an existing database cluster; CNPG
  bootstrap semantics apply.

See [Backup and Restore](backup-restore.md) for scheduled backup policy and
[Troubleshooting](troubleshooting.md) for status and Event inspection.
