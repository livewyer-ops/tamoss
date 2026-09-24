# Upgrades

Upgrade TAMOSS by applying source-controlled platform, operator, and environment
overlay changes in order. From `8.2.0-oss2`, each release publishes
`dependencies.yaml` with its tested managed-platform versions, alongside `compatibility.yaml` and
`release.json`. The release record includes checksums for both manifests.
Use the platform configuration, operator and instance images from the same
release, and follow its declared predecessor in `compatibility.yaml`.

The direct upgrade to `8.2.0-oss2` starts at `8.2.0-oss1` and advances the database
schema to `8.2.0-oss2`. Upgrade older installations through their supported
OSS releases first. External services remain managed by their owners; compare
their versions and configuration with the target release before deployment.

## Before upgrading

1. Confirm PostgreSQL and object-storage backups are recent and restorable.
2. Read the TAMOSS changelog.
3. Diff manifests for the target environment in a non-production environment.
4. Review platform overrides against the target charts. For this release,
   Traefik chart 41 renames logging values and changes file-provider content to
   an object; cert-manager 1.21 changes monitoring values and controller
   service-account permissions. Check the [Traefik chart notes](https://github.com/traefik/traefik-helm-chart/releases/tag/v41.0.0)
   and [cert-manager notes](https://github.com/cert-manager/cert-manager/releases/tag/v1.21.0)
   if the environment customises these settings.
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

CNPG controller upgrades can trigger a [rolling restart of database Pods](https://cloudnative-pg.io/docs/1.30/rolling_update/).
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

Before upgrading from RustFS `1.0.0-beta.3` to `1.0.0`, back up media and
credentials, verify restoration, and pause ingest and other media writes.
Preserve Tenant names, pools, PVCs and credentials. If credentials still use
`rustfsadmin`, rotate them first: RustFS 1.0.0 rejects the legacy defaults.
Generated TAMOSS credentials do not need replacement.

This release retains RustFS operator `0.0.1`. The TAMOSS operator defaults the
runtime to `1.0.0`; update any explicit
`spec.backends.s3.rustfsOperator.image` override to `rustfs/rustfs:1.0.0` in the
environment configuration before applying the normal sequence above. Wait for
the StatefulSet rollout and `Ready=True`, then check existing media, uploads,
copy and deletion before resuming writes. Recover by restoring the pre-upgrade
backup into a matching installation; do not downgrade an upgraded data volume.

RustFS 1.0.0 rejects unsigned `x-amz-*` checksum headers added to presigned URLs,
including `x-amz-checksum-mode: ENABLED`. Include these headers when signing
requests; presigned uploads also support `Content-MD5`. Existing objects and
their stored checksums remain readable without recalculation.

Local Compose uses `tamoss-local` and `tamoss-local-secret` by default. Override
them with `TAMOSS_S3_ACCESS_KEY` and `TAMOSS_S3_SECRET_KEY` for both Compose and
native development commands. Preserve the existing volume when recreating the
container; `docker compose down --volumes` deletes it.

## Authentik

This release updates Authentik directly from `2026.2.3` to `2026.2.7`, using
chart `2026.2.3` and its native image override. Its PostgreSQL database stays on
major 17. Back up that database, the Authentik secret key and credentials before
applying the platform layer. Preserve existing users and provider identities;
verify login, logout and OAuth client access after the server and worker
rollouts. Separate outposts must use the same Authentik version as the server.

Platform updates stop on failure without automatic Helm rollback. Keep the
failed release available for diagnosis and correct it forward, or restore the
verified backup into a matching installation. Reverting chart or image versions
does not undo database migrations.

## PostgreSQL statistics

PostgreSQL 18.6 fixes incorrect row estimates after parallel GIN index creation.
After upgrading a database from an earlier 18.x release, inspect tables with GIN
indexes using the query in the [PostgreSQL release notes](https://www.postgresql.org/docs/release/18.6/).
Run `ANALYZE schema_name.table_name` for tables with incorrect estimates,
including `Infinity` or `NaN`. This refreshes statistics without rewriting media
or changing the TAMOSS schema.

## Upgrading a pinned environment instance

Environment instances can pin `spec.api.image.tag`, `spec.ui.image.tag` and
`spec.console.image.tag`. Update all configured pins to the candidate's matching
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

The move from `8.1.0-oss6` to 8.2 changes both API and schema. Follow the full
platform, operator and instance sequence above, using the compatibility metadata
and image references from the same release. Validate existing Sources, Flows,
media, webhooks and queued work after the migration. A fresh-install check does
not exercise this upgrade path.

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

The `8.2.0-oss2` migration widens Segment timestamp bounds from `BIGINT` to
`NUMERIC`, preserving exact nanoseconds. Existing values convert without
recalculation; media objects and checksums are unchanged. PostgreSQL rewrites
the Segment table and its indexes, so reserve a maintenance window and enough
temporary database space for the rewrite. Duration depends on Segment volume.
The migration refreshes table statistics before completing.

The preceding database revision is `20260810_0007`; the new revision is
`20260924_0008`. The previously published `8.2.0-oss2-rc2` also uses the preceding
schema and follows this migration path. Keep API and operator images from the
same candidate. Schema changes and the Alembic revision update are transactional;
after a failure, investigate the cause and use the existing schema retry action.

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
