# Upgrades

Upgrade TAMOSS by applying source-controlled platform, operator, and environment
overlay changes in order. Read the target release's [changelog](../../CHANGELOG.md)
entry for migration prerequisites and dependency-specific actions. Use its
`compatibility.yaml` for the supported predecessor and schema revision,
`dependencies.yaml` for tested managed-platform versions, and `release.json`
for image references and artefact checksums.

Use the platform configuration, operator and instance images from the same
release. Upgrade older installations through their declared predecessors.
External services remain managed by their owners; compare their versions and
configuration with the target release before deployment.

## Before upgrading

1. Confirm PostgreSQL and object-storage backups are recent and restorable.
2. Read the target release's changelog entry and upstream dependency upgrade notes.
3. Diff manifests for the target environment in a non-production environment.
4. Review platform overrides against the target charts, including renamed
   values, credentials and permissions.
5. Confirm the current `Tamoss` resource reports `Ready=True` and
   `Upgradeable=True`.

For local validation, `task kind:e2e PROFILE=local-kind` creates a fresh
[Kind](https://kind.sigs.k8s.io/)
cluster with the current operator image and runs the deployed TAMS API and UI
checks.

## Sequence

1. For a schema upgrade, set `spec.paused: true` in each affected instance's
   environment overlay and apply the instance layer with
   `task env:instance:apply`. Wait for `Paused=True` before replacing the operator.
   On a shared cluster, pause every instance that is not yet staged for the new
   schema. Pausing reconciliation does not stop existing workloads.
2. Update the source-controlled platform, operator, or environment overlay
   files.
3. Diff the target platform, operator, and environment overlay.
4. Apply the platform, operator, and environment layers through the checked-in
   environment workflow, retaining the pause while staging matching API and
   operator images. The schema Job runs from the instance's API image, so that
   image must contain the revision requested by the operator.
5. During the maintenance window, set `spec.paused: false` and apply the instance
   layer. The operator runs the migration and restores the API and worker
   Deployments after it succeeds. Allow for API unavailability during this step.
6. Wait for `SchemaMigrated=True` and `Ready=True`.
7. Check `status.schemaMigration` for the final attempt result.
8. Run deployed checks.

```bash
export KUBECONFIG=/path/to/kubeconfig
export TAMOSS_ENV=my-prod

task env:diff ENV="$TAMOSS_ENV" KUBECONFIG="$KUBECONFIG"

task env:apply ENV="$TAMOSS_ENV" KUBECONFIG="$KUBECONFIG"
# For a schema upgrade, now set spec.paused: false in the staged overlay.
task env:instance:apply ENV="$TAMOSS_ENV" KUBECONFIG="$KUBECONFIG"
task env:wait ENV="$TAMOSS_ENV" KUBECONFIG="$KUBECONFIG"
task env:status ENV="$TAMOSS_ENV" KUBECONFIG="$KUBECONFIG"
task e2e:deployed PROFILE=multi-server KUBECONFIG="$KUBECONFIG"
```

`kubectl diff` exits with code 1 when differences are found; that is expected
during review.

CNPG controller upgrades can trigger a rolling restart of database Pods.
Plan downtime for a single-instance database, including its graceful shutdown
period. Wait for the CNPG `Cluster` to report `Ready=True` before running the
deployed checks; an available controller Deployment does not establish database
readiness.

If automation cannot call Task, keep the same source-controlled inputs and
apply the same layers in the same order:

```bash
(
  cd deploy/platform
  helmfile --kubeconfig "$KUBECONFIG" \
    --file helmfile.yaml.gotmpl \
    --state-values-file values/defaults.yaml \
    --state-values-file "../../deploy/environments/$TAMOSS_ENV/platform-values.yaml" \
    sync \
    --sync-args "--server-side=true" \
    --wait \
    --wait-for-jobs
)
kubectl --kubeconfig "$KUBECONFIG" apply --server-side -k deploy/operator
kubectl --kubeconfig "$KUBECONFIG" apply -k "deploy/environments/$TAMOSS_ENV"
```

## RustFS

Before upgrading RustFS, back up media and credentials, verify restoration, and
pause ingest and other media writes. Preserve Tenant names, pools, PVCs and
credentials, applying any credential changes required by the release notes.

Update any explicit `spec.backends.s3.rustfsOperator.image` override to the
target release's tested image before applying the normal sequence above. Wait for
the StatefulSet rollout and `Ready=True`, then check existing media, uploads,
copy and deletion before resuming writes. Recover by restoring the pre-upgrade
backup into a matching installation; do not downgrade an upgraded data volume.

Local Compose uses `tamoss-local` and `tamoss-local-secret` by default. Override
them with `TAMOSS_S3_ACCESS_KEY` and `TAMOSS_S3_SECRET_KEY` for both Compose and
native development commands. Preserve the existing volume when recreating the
container; `docker compose down --volumes` deletes it.

## Authentik

Follow Authentik's supported upgrade sequence for the target version. Back up
its database, secret key and credentials before applying the platform layer.
Preserve existing users and provider identities;
verify login, logout and OAuth client access after the server and worker
rollouts. Separate outposts must use the same Authentik version as the server.

Platform updates stop on failure without automatic Helm rollback. Keep the
failed release available for diagnosis and correct it forward, or restore the
verified backup into a matching installation. Reverting chart or image versions
does not undo database migrations.

## Upgrading a pinned environment instance

Environment instances can pin `spec.api.image.tag`, `spec.ui.image.tag` and
`spec.console.image.tag`. Update all configured pins to the target release's matching
images; the worker uses the API image. For an image-only update supported by the
installed operator and schema, apply the instance layer and wait for `Ready=True`:

```bash
task env:instance:apply ENV="$TAMOSS_ENV" KUBECONFIG="$KUBECONFIG"
task env:wait ENV="$TAMOSS_ENV" KUBECONFIG="$KUBECONFIG"
```

Upgrade one instance at a time on shared clusters. Pin all instance images if
they must remain unchanged while updating the TAMOSS operator; omitted image
tags follow the installed operator's defaults. Explicit PostgreSQL and RustFS
pins also need updating to the selected release versions.

When the target release changes the schema, follow the full sequence above.
Validate existing Sources, Flows, media, webhooks and queued work after the
migration. A fresh-install check does not exercise an upgrade path.

## Status Checks

```bash
kubectl --kubeconfig "$KUBECONFIG" -n tams describe tamoss tamoss-multi-server
kubectl --kubeconfig "$KUBECONFIG" -n tams get tamoss tamoss-multi-server \
  -o jsonpath='{.status.upgrade}{"\n"}{.status.schemaMigration}{"\n"}{.status.resolved.versions}{"\n"}'
```

`UnsupportedSchemaVersion` means the database revision is not the current
release revision. Stop before rolling workloads forward and investigate the
database state. `SchemaMigrationFailed` means the migration Job failed
repeatedly; investigate PostgreSQL connectivity, permissions, and migration logs
before applying another desired state.

`status.schemaVersion`, `status.schemaMigration.appliedRevision`, and
`status.resolved.versions.schema` identify the applied TAMOSS database schema
revision. `status.schemaMigration.supportedTAMSAPI` and
`status.resolved.versions.tamsAPI` identify the BBC TAMS API compatibility
level; they are not the TAMOSS product version.

## Schema Migrations

Review the target release's migration notes for locking, table rewrites,
temporary disk space and expected downtime. Rehearse the upgrade with a
restored database to estimate the maintenance window.

Keep API and operator images from the same release. Schema changes and the
Alembic revision update are transactional; after a failure, investigate the
cause and use the existing schema retry action.

The operator schema Job runs the TAMOSS application migration CLI from the API
image:

```bash
tamoss-db migrate
```

Non-Kubernetes operators use the same command with the current PostgreSQL
component environment:

```bash
POSTGRES_HOST=postgres POSTGRES_USER=tamoss POSTGRES_PASSWORD=secret POSTGRES_DB=tams tamoss-db migrate
```

Fresh installs run from an empty database to the current head.

## Operator Manifests

`task operator:template` renders the checked-in
[Kustomize](https://kustomize.io/) operator install
for review:

```bash
task operator:template
```

## Rollback

For image-only changes compatible with the current schema, roll back by
reverting source-controlled manifests and reapplying the changed layer.

The operator does not automatically roll back application images or database
schema. Schema downgrades are unsupported. Restore the PostgreSQL backup into
a matching installation if a completed schema upgrade must be reversed.

For short investigations, pause reconciliation before manual edits and resume
afterwards. See [Day 2 Operations](day-2.md).
