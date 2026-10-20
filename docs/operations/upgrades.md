# Upgrades

Each `Tamoss` instance selects an exact product release through `spec.version`.
Installing a newer operator leaves that selection unchanged. To upgrade an
instance, change its version in its source-controlled manifest and apply it.
Explicit component image overrides continue to take precedence.

The TAMOSS source revision for a release contains the platform dependency pins
and the matching operator install reference. Use that revision when applying a
shared platform or operator update. Read the target release's
[changelog](../../CHANGELOG.md) entry first: `compatibility.yaml` declares
supported upgrade paths, `dependencies.yaml` records tested platform versions,
`catalogue.json` records instance releases supported by the operator, and
`release.json` includes image references and artefact checksums.
The platform `helmfile apply` command requires the
[Helm diff plugin](https://github.com/databus23/helm-diff). Install it with
`helm plugin install https://github.com/databus23/helm-diff --verify=false` if it is absent.

## Prepare an existing instance

Before installing an operator that requires release selection, pin
`spec.version` to the instance's installed release in its environment overlay.
Use the release record and existing workload images to identify it. An instance
without a version reports `VersionRequired`; its workloads continue running,
but reconciliation and new managed work wait for an explicit pin.

Review existing image overrides. Remove an override only when that component
should follow the selected release. This includes
`spec.backends.s3.rustfsOperator.image` and
`spec.backends.db.cnpg.postgresVersion`. PostgreSQL values previously inserted
by CRD defaulting also remain overrides until removed. An API image override
must contain the migration revision required by the selected release.

## Upgrade sequence

1. Read the release prerequisites, verify restorable backups, and rehearse the
   supported upgrade with populated data in a separate environment.
2. Check out the source revision for the target release. Update the remote
   resource in `operator/kustomization.yaml` to that release's published
   `install.yaml`, then review the platform and operator changes.
3. Apply the platform with Helmfile, then apply the operator Kustomization and
   wait for its CRDs and Deployment. Confirm its catalogue supports every
   instance release still in use. External database and storage services remain
   their owners' responsibility.
4. During the maintenance window, stop ingest and other writes as required by
   the release notes. Set the chosen instance's `spec.version` to the target
   release. If it is paused, clear `spec.paused` when ready to proceed.
5. Review and apply only that instance's Kustomize directory, then confirm
   `status.currentVersion` matches the requested release and `Ready=True`.
6. Validate existing media, uploads, metadata, webhooks and queued work before
   resuming normal traffic.

The operator reconciles managed backends first, waits for their requested
rollouts, then completes the schema migration before reconciling application
workloads. Existing ingest Jobs retain their original image; new Jobs wait for
the instance to become ready. Pausing reconciliation does not stop workloads
or active writes.

Upgrade one instance at a time. Other pinned instances retain their release
images and schema targets. Shared platform changes can still affect them; for
example, a CNPG controller upgrade can restart database Pods. Allow downtime
for a single-instance database.

```bash
export KUBECONFIG=/path/to/kubeconfig
export TAMOSS_ENV=my-prod
export INSTANCE=prod-a

# Run from the source revision for the target release. Update the operator
# install URL in operator/kustomization.yaml before applying it.
$EDITOR "deploy/environments/$TAMOSS_ENV/operator/kustomization.yaml"
(
  cd deploy/platform
  helmfile --kubeconfig "$KUBECONFIG" \
    --file helmfile.yaml.gotmpl \
    --state-values-file values/defaults.yaml \
    --state-values-file "../environments/$TAMOSS_ENV/platform-values.yaml" \
    apply \
    --skip-diff-on-install \
    --sync-args "--server-side=true" \
    --wait \
    --wait-for-jobs
)
kubectl --kubeconfig "$KUBECONFIG" apply --server-side -k "deploy/environments/$TAMOSS_ENV/operator"
kubectl --kubeconfig "$KUBECONFIG" wait --for=condition=Established crd/tamosses.tamoss.livewyer.io --timeout=60s
kubectl --kubeconfig "$KUBECONFIG" -n tamoss-system rollout status deployment/operator-controller-manager --timeout=5m

# Edit spec.version in instances/$INSTANCE/tamoss.yaml.
kubectl --kubeconfig "$KUBECONFIG" diff -k "deploy/environments/$TAMOSS_ENV/instances/$INSTANCE"
kubectl --kubeconfig "$KUBECONFIG" apply -k "deploy/environments/$TAMOSS_ENV/instances/$INSTANCE"
task env:wait ENV="$TAMOSS_ENV" INSTANCE="$INSTANCE" KUBECONFIG="$KUBECONFIG"
task env:status ENV="$TAMOSS_ENV" INSTANCE="$INSTANCE" KUBECONFIG="$KUBECONFIG"
```

`task env:diff`, `task env:instance:apply`, `task env:wait` and `task env:status`
remain optional convenience commands. `kubectl diff` exit code 1 means changes
were found; values greater than 1 indicate an error.

## Installation defaults

To change shared defaults, edit the environment's `operator/defaults.yaml`,
review the diff and apply the operator overlay as above. Its generated ConfigMap
changes the Pod template, causing a rollout that loads the new settings.
Changing shared defaults affects every instance inheriting those fields,
independently of its pinned release. Review that change separately from a
release update and keep the platform's DNS and TLS configuration aligned.

After the operator rollout, run `task env:wait` for the environment. It checks
the observed instance generation and requested release, then checks
`status.appliedDefaultsRevision` against the non-empty environment defaults
file before accepting `Ready=True`.

Existing explicit settings remain overrides. This includes persisted CRD
defaults such as `spec.console.enabled: false`; remove that field only if the
instance should inherit the shared Console setting. Keep the existing resource
name, namespace and immutable `spec.fullnameOverride`. Preserve endpoint and TLS
overrides to retain public addresses and certificates. The minimal resource is
the starting point for new instances; adoption does not require removing
existing configuration.

## Managed storage and identity

Managed RustFS follows `spec.version` when its image override is absent.
Preserve Tenant names, pools, PVCs and credentials. TAMOSS does not provide a
default backup for managed RustFS data. Use a backup or replication method
supported by the storage platform, and test restoring the complete Tenant
before relying on it. Follow release-specific credential instructions, wait
for the StatefulSet rollout, and test existing media and new writes. Do not
downgrade an upgraded data volume.

Authentik follows the shared platform release. Follow its supported upgrade
sequence and preserve its database, secret key, users and provider identities.
Verify login, logout and OAuth client access after server and worker rollouts.
Separate outposts must match the server version.

Platform updates stop on failure without automatic Helm rollback. Diagnose and
correct the failed release, or restore the verified backup into a matching
installation. Reverting image versions does not undo data migrations.

## Status and failed upgrades

```bash
kubectl --kubeconfig "$KUBECONFIG" -n tams get tamoss <instance> \
  -o jsonpath='{.spec.version}{"\n"}{.status.currentVersion}{"\n"}{.status.upgrade}{"\n"}{.status.schemaMigration}{"\n"}'
```

`status.resolved.versions.tamoss` is the selected release;
`status.resolved.images` shows effective images, including overrides.
`status.currentVersion` advances only after the schema and workload rollouts
succeed. `status.schemaVersion` records the applied product schema, while
`status.resolved.versions.schema` is the selected target. The BBC TAMS API
version is recorded separately under `status.resolved.versions.tamsAPI`.

`UnsupportedVersion` means the installation catalogue does not contain the
requested release. `UnsupportedUpgrade` rejects a skipped path or downgrade.
`VersionAdoptionRequired` asks for the installed release to be pinned before
starting an upgrade. `UnsupportedSchemaVersion` means the installed schema is
outside the selected release's supported starting revisions. `UpgradeInProgress`
requires the target recorded in `status.upgrade.targetVersion` to finish before
selecting another release.

For `SchemaMigrationFailed`, inspect migration Job logs, database connectivity
and permissions. Correct the cause and use the existing schema retry action.
Keep the requested release selected while an upgrade is in progress.

## Schema migrations and recovery

Rehearse migrations with a restored database to estimate locking, disk-space
requirements and downtime. The migration Job uses the selected API image and
its catalogue Alembic revision. Schema changes and the revision update are
transactional. The operator runs the migration and reports its progress in
`Tamoss.status.schemaMigration`; inspect the Job logs if it fails.

The operator does not automatically roll back images or schema. Product release
downgrades are unsupported. Restore the PostgreSQL and storage backups into a
matching installation when a completed upgrade must be reversed. For temporary
manual diagnosis, pause reconciliation as described in [Day 2 Operations](day-2.md).
