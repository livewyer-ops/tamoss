# Upgrades

Each `Tamoss` instance selects an exact product release through `spec.version`.
To upgrade an instance, change its version in its source-controlled manifest
and apply it. Explicit component image overrides take precedence.

Use the target release's source revision for platform dependency pins and its
published `install.yaml` asset for the operator. Checked-in operator manifests
and `operator/config/releases/catalogue.json` are development configuration,
including in release-tag checkouts. Updating the operator image alone does not
update its catalogue. Read the target release's
[changelog](../../CHANGELOG.md) entry first: `compatibility.yaml` declares
supported upgrade paths, `dependencies.yaml` records tested platform versions,
`catalogue.json` records instance releases supported by the operator, and
`release.json` includes image references and artefact checksums.
The platform `helmfile apply` command requires
[Helm diff](install.md#helm-diff).

## Environment layout migration

Older generated environments combine a shared base with `tamoss-patch.yaml`.
The patch contains only overrides, so preserve the fully rendered resources:

1. Render the existing environment with `kubectl kustomize` before changing
   its base or directory layout. Save the output for comparison.
2. Put the complete rendered `Tamoss` in `instances/<instance>/tamoss.yaml`,
   with its instance-owned Namespace in `namespace.yaml`. Preserve names,
   namespaces, `fullnameOverride`, version pins and custom settings. Carry over
   any additional instance resources; keep shared resources at the root.
3. List these files in the instance's `kustomization.yaml`. Replace the old
   base and patch references in the environment's top-level Kustomization with
   `instances/<instance>`, preserving resources shared by other instances.
4. Compare the rendered resources before and after the move; they must be
   equivalent. Create the separate operator overlay from
   `deploy/templates/environment/operator`, replacing `__VERSION__` with the
   target release in its Kustomization and replacing the profile and domain
   placeholders in `defaults.yaml` with the existing site's settings. Preserve
   existing installation defaults and operator patches. Keep `operator/` and
   `platform-values.yaml` at the environment root, as shown in the
   [environment layout](install.md#existing-cluster).

`task env:apply` and `task env:diff` require this operator overlay and render it
before running platform commands. A missing overlay stops the task.
`OPERATOR_KUSTOMIZE_DIR` explicitly selects a different overlay; local
development normally uses `task kind:up`.

## Prepare an existing instance

Identify the installed release from its release record and workload images.
Prepare `spec.version` with that release in the instance manifest. Schema
versions alone do not identify a product release: multiple releases can share
the same schema.

If the installed CRD does not accept `spec.version`, install the new CRD and
operator before applying this pin. An unpinned instance then reports
`VersionRequired`; its workloads continue running, but reconciliation and new
managed work wait. Apply the installed-release pin and wait for
`status.currentVersion` to match and `Ready=True` before changing it to the
target release. See the [release-specific adoption steps](../../CHANGELOG.md).

For this legacy case, render the prepared instance locally with
`kubectl kustomize` and defer its server-side diff until the new CRD is installed.
`task env:diff` includes an instance diff, which the old CRD cannot validate
while the prepared manifest contains `spec.version`.

Review existing image overrides. Remove an override only when that component
should follow the selected release. This includes
`spec.backends.s3.rustfsOperator.image` and
`spec.backends.db.cnpg.postgresVersion`. PostgreSQL values previously inserted
by CRD defaulting also remain overrides until removed. An API image override
must contain the migration revision required by the selected release.

## Upgrade sequence

1. Read the release prerequisites, identify every installed instance release,
   verify restorable backups, and rehearse the supported upgrade with populated
   data in a separate environment.
2. Check out the source revision for the target release. Update the remote
   resource in `operator/kustomization.yaml` to that release's published
   `install.yaml`, then render and review the platform and operator changes.
   Confirm the target catalogue supports every instance release still in use.
3. Start the maintenance window and stop ingest and other writes as required
   by the release notes. Shared platform changes can restart databases, storage
   and identity services before an instance version changes.
4. Apply the platform with `task env:platform:apply`, which validates all Helm
   releases before changing the cluster. The platform task waits for platform
   controllers; also check managed database, storage and identity rollouts and
   existing application access. Then apply the operator Kustomization and wait
   for its CRDs and Deployment. External services remain their owners'
   responsibility.
5. Complete adoption of any unpinned legacy instances as described above.
6. Set the chosen instance's `spec.version` to the target release. If it is
   paused, clear `spec.paused` when ready. Review and apply only that instance's
   Kustomize directory, then confirm the requested generation has been observed,
   `status.currentVersion` matches the requested release and `Ready=True`.
7. Validate authenticated API access, existing media, uploads, metadata,
   webhooks and queued work before resuming normal traffic.

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
kubectl kustomize "deploy/environments/$TAMOSS_ENV/operator" >/dev/null
kubectl --kubeconfig "$KUBECONFIG" diff --server-side -k "deploy/environments/$TAMOSS_ENV/operator"

# Start maintenance and stop writes before this command.
task env:platform:apply ENV="$TAMOSS_ENV" KUBECONFIG="$KUBECONFIG"
# Verify backend recovery and existing application access before continuing.
kubectl --kubeconfig "$KUBECONFIG" apply --server-side -k "deploy/environments/$TAMOSS_ENV/operator"
kubectl --kubeconfig "$KUBECONFIG" wait --for=condition=Established crd/tamosses.tamoss.livewyer.io --timeout=60s
kubectl --kubeconfig "$KUBECONFIG" -n tamoss-system rollout status deployment/operator-controller-manager --timeout=5m

# For an unadopted instance, first set spec.version to its INSTALLED release,
# apply its directory and run env:wait. Only then select the target release.
$EDITOR "deploy/environments/$TAMOSS_ENV/instances/$INSTANCE/tamoss.yaml"
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

Platform updates stop on failure without automatic Helm rollback. Diagnose and
correct the failed release, or restore the verified backup into a matching
installation. Reverting image versions does not undo data migrations.

### RustFS

Managed RustFS follows `spec.version` when its image override is absent.
Preserve Tenant names, pools, PVCs and credentials. TAMOSS does not provide a
default backup for managed RustFS data. Use a backup or replication method
supported by the storage platform, and test restoring the complete Tenant
before relying on it. Wait for the StatefulSet rollout, and test existing media
and new writes. Do not downgrade an upgraded data volume.

Generated TAMOSS credentials need no rotation. Check the target release's
[credential prerequisites](../../CHANGELOG.md) before updating RustFS. Invalid
credentials can interrupt storage; correct the configuration to resume the
upgrade. TAMOSS does not replace user-owned Secrets.

Include any `x-amz-*` checksum headers, such as
`x-amz-checksum-mode: ENABLED`, when signing presigned URLs. RustFS rejects
unsigned additions. Presigned uploads also support `Content-MD5`.

### Authentik

Authentik follows the shared platform release. Follow its supported upgrade
sequence and preserve its database, secret key, users and provider identities.
Verify login, logout and OAuth client access after server and worker rollouts.
Separate outposts must match the server version.

## Status and failed upgrades

```bash
kubectl --kubeconfig "$KUBECONFIG" -n tams get tamoss <instance> \
  -o jsonpath='{.spec.version}{"\n"}{.status.currentVersion}{"\n"}{.status.upgrade}{"\n"}{.status.schemaMigration}{"\n"}'
```

`spec.version` is the requested release. `status.resolved.versions.tamoss` and
`status.resolved.images` describe the last resolved release and effective images,
including overrides; they can retain the previous values when selection is
blocked.
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

`task env:status` and `task env:summary` show the requested and completed product
releases, applied schema and upgrade message. `task env:wait` checks the observed
generation and stops on release-selection blockers, printing these diagnostics.
A successful `schemaMigration` record can describe a previous release. It is
retained when a new request is blocked and does not establish that the requested
upgrade has started. Readiness must be followed by application and data checks
before reopening traffic.

### Diagnose an unsupported release

Inspect the running operator and the catalogue configured on its Deployment:

```bash
kubectl --kubeconfig "$KUBECONFIG" -n tamoss-system get pods -l control-plane=controller-manager \
  -o custom-columns='NAME:.metadata.name,IMAGES:.spec.containers[*].image,READY:.status.containerStatuses[*].ready,CONFIGMAPS:.spec.volumes[*].configMap.name'
kubectl --kubeconfig "$KUBECONFIG" -n tamoss-system rollout status deployment/operator-controller-manager --timeout=5m
TAMOSS_CATALOGUE_CM=$(kubectl --kubeconfig "$KUBECONFIG" -n tamoss-system get deployment operator-controller-manager \
  -o jsonpath='{.spec.template.spec.volumes[?(@.name=="release-catalogue")].configMap.name}')
kubectl --kubeconfig "$KUBECONFIG" -n tamoss-system get configmap "$TAMOSS_CATALOGUE_CM" \
  -o jsonpath='{.data.catalogue\.json}{"\n"}'
```

The operator loads the catalogue at startup. During an incomplete rollout, the
Deployment's configured catalogue may differ from the running Pod's catalogue.
Compare it with the target release's published `catalogue.json` asset.
If the target is absent, update the environment overlay to a published operator
bundle supporting the target and all existing instance releases, apply the full
overlay and wait for rollout. Preserve custom defaults and patches. Complete
legacy adoption before requesting the target again. Do not rename a `dev`
catalogue entry: its images and schema also describe a development build.

If RustFS fails during rollout, inspect the Tenant conditions and the RustFS
Pod's events and logs. Schema and application updates wait for backend
readiness. Correct invalid custom configuration to resume the upgrade; storage
may be unavailable until the rollout recovers. TAMOSS does not replace
user-owned Secrets. Check the [RustFS credential requirements](#rustfs).

For `SchemaMigrationFailed`, inspect migration Job logs, database connectivity
and permissions. Correct the cause and use the existing schema retry action.
Keep the requested release selected while an upgrade is in progress.

## Schema migrations and recovery

Rehearse migrations with a restored database to estimate locking, disk-space
requirements and downtime. Follow the migration prerequisites in the target
release's [changelog entry](../../CHANGELOG.md). The migration Job uses the selected API image and
its catalogue Alembic revision. Schema changes and the revision update are
transactional. The operator runs the migration and reports its progress in
`Tamoss.status.schemaMigration`; inspect the Job logs if it fails.
The schema-state ConfigMap records the completed product schema. Alembic records
the database revision and skips migrations already applied.

The operator does not automatically roll back images or schema. Product release
downgrades are unsupported. Restore the PostgreSQL and storage backups into a
matching installation when a completed upgrade must be reversed. For temporary
manual diagnosis, pause reconciliation as described in [Day 2 Operations](day-2.md).
