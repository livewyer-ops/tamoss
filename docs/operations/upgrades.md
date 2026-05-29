# Upgrades

Upgrade TAMOSS by applying source-controlled platform, operator, and environment
overlay changes in order.

## Before Upgrading

1. Confirm PostgreSQL and object-storage backups are recent and restorable.
2. Read the TAMOSS changelog.
3. Diff manifests for the target environment in a non-production environment.
4. Confirm any platform dependency change is intentional.
5. Confirm the current `Tamoss` resource reports `Ready=True` and
   `Upgradeable=True`.

For local validation, `task kind:e2e PROFILE=local-kind` creates a fresh Kind
cluster, proves the deployed API/UI workflows, then upgrades the same cluster
from the previous operator image to the current operator image.

## Sequence

1. Update the source-controlled platform, operator, or environment overlay
   files.
2. Diff the target platform, operator, and environment overlay.
3. Apply the platform, operator, and environment layers through the checked-in
   environment workflow.
4. Wait for `SchemaMigrated=True` and `Ready=True`.
5. Check `status.schemaMigration` for the final attempt result.
6. Run deployed checks.

```bash
export KUBECONFIG=/path/to/kubeconfig
export TAMOSS_ENV=my-prod

kubectl --kubeconfig "$KUBECONFIG" diff -k deploy/platform/multi-server || true
kubectl --kubeconfig "$KUBECONFIG" diff --server-side -k deploy/operator || true
kubectl --kubeconfig "$KUBECONFIG" diff -k "deploy/environments/$TAMOSS_ENV" || true

task k8s:apply ENV="$TAMOSS_ENV" KUBECONFIG="$KUBECONFIG"
task k8s:wait ENV="$TAMOSS_ENV" KUBECONFIG="$KUBECONFIG"
task k8s:status ENV="$TAMOSS_ENV" KUBECONFIG="$KUBECONFIG"
task e2e:deployed PROFILE=multi-server KUBECONFIG="$KUBECONFIG"
```

`kubectl diff` exits with code 1 when differences are found; that is expected
during review.

If automation cannot call Task, keep the same source-controlled inputs and
apply the rendered layers in the same order:

```bash
kubectl --kubeconfig "$KUBECONFIG" apply -k deploy/platform/multi-server
kubectl --kubeconfig "$KUBECONFIG" apply --server-side -k deploy/operator
kubectl --kubeconfig "$KUBECONFIG" apply -k "deploy/environments/$TAMOSS_ENV"
```

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

## Operator Artifacts

Maintainer commands:

```bash
task operator:template
```

`task operator:template` renders the Kustomize operator install.

## Rollback

For planned changes, roll back by reverting source-controlled manifests and
reapplying the changed layer.

The operator does not automatically roll back application images or database
schema. Restore provider data from your backup plan when a failed migration
requires a data rollback.

For short investigations, pause reconciliation before manual edits and resume
afterward. See [Day 2 Operations](day-2.md).
