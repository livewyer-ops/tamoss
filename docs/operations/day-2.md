# Day 2 Operations

Start Day 2 work from the `Tamoss` resource. It is the operator's summary of
schema, backend, identity, rollout, and degradation state.

```bash
export KUBECONFIG=/path/to/kubeconfig
export TAMOSS_NAMESPACE=tams
export TAMOSS_NAME=tamoss-multi-server

kubectl --kubeconfig "$KUBECONFIG" -n "$TAMOSS_NAMESPACE" get tamoss "$TAMOSS_NAME" -o wide
kubectl --kubeconfig "$KUBECONFIG" -n "$TAMOSS_NAMESPACE" describe tamoss "$TAMOSS_NAME"
kubectl --kubeconfig "$KUBECONFIG" -n "$TAMOSS_NAMESPACE" get events --sort-by=.lastTimestamp
kubectl --kubeconfig "$KUBECONFIG" -n tamoss-system logs deploy/operator-controller-manager --tail=200
```

## Conditions

`Tamoss` conditions:

| Condition | Meaning |
| --- | --- |
| `Ready` | Enabled workloads and required integrations are available. |
| `Progressing` | The operator is applying resources or waiting for rollout. |
| `BackendsReady` | Database, S3, [Authentik](https://goauthentik.io/), and dependency references are configured. |
| `BackupPolicyReady` | Managed [CNPG](https://cloudnative-pg.io/) backup policy and continuous archiving are ready, disabled, external, or report the reason they are not usable. |
| `SchemaMigrated` | The embedded database schema has been applied. |
| `IdentityBlueprintSubmitted` | Managed Authentik blueprint submission has completed or is not required. |
| `IdentityReady` | Managed or external OAuth/OIDC readiness checks pass. |
| `Upgradeable` | The desired schema state can complete safely. |
| `RoutingReady` | Managed Ingress or Gateway API routes are configured and accepted. |
| `HostnamesReady` | Managed routing hostnames are configured or admitted by the Gateway controller. |
| `LifecycleReady` | No hibernate/resume lifecycle state is gating reconciliation; `False` while the instance is `Hibernating`, `Hibernated`, `Resuming`, or a lifecycle operation has failed. |
| `Degraded` | A user-actionable or terminal reconcile problem occurred. |
| `Paused` | `.spec.paused=true`; write reconciliation is suspended. |

`StorageBackend` conditions:

| Condition | Meaning |
| --- | --- |
| `Ready` | The storage backend is usable by TAMOSS. |
| `BucketReady` | The managed or external object-store bucket is reachable for the selected ownership mode. |
| `DatabaseReady` | The backend registration row exists in the TAMOSS database. |
| `ExternalS3DiagnosticReady` | Best-effort external S3 browser-upload diagnostics have completed, skipped, or reported a warning reason. |

`FlowProfile` conditions:

| Condition | Meaning |
| --- | --- |
| `Ready` | The exact immutable definition is registered or adopted. |
| `Registered` | The operator has confirmed the desired TAMS Profile registration. |
| `DeletionBlocked` | A Flow still references the Profile, or another live resource claims the UUID. |

Use `kubectl describe flowprofile <name>` to inspect stable reasons such as
`InvalidProfile`, `ProfileConflict`, `DuplicateProfileID`, and `ProfileInUse`.

Inspect schema migration attempts without reading Jobs first:

```bash
kubectl --kubeconfig "$KUBECONFIG" -n "$TAMOSS_NAMESPACE" \
  get tamoss "$TAMOSS_NAME" -o jsonpath='{.status.schemaMigration}'
```

If schema migration reaches a terminal failure after repeated attempts, trigger
one explicit retry by changing the retry annotation value:

```bash
kubectl --kubeconfig "$KUBECONFIG" -n "$TAMOSS_NAMESPACE" annotate tamoss "$TAMOSS_NAME" \
  tamoss.livewyer.io/schema-retry="$(date -Iseconds)" --overwrite
```

The operator records the consumed value, clears the schema failure counter, and
deletes the failed migration Job before launching a new attempt. The operator
ignores a reused annotation value.

## Observability

The operator exposes Prometheus metrics from the manager metrics service. The
optional `operator/config/prometheus` overlay renders a `ServiceMonitor` and
alert rules when Prometheus Operator CRDs are available.

Render and validate the overlay:

```bash
task operator:monitoring:check
kubectl --kubeconfig "$KUBECONFIG" apply -k operator/config/prometheus
```

Key operator metrics:

| Metric | Use |
| --- | --- |
| `tamoss_resource_condition` | Mirrors `Tamoss` and `StorageBackend` status conditions. |
| `tamoss_provider_ready` | Shows readiness for database, S3, auth, and routing provider domains. |
| `tamoss_reconcile_duration_seconds` | Measures reconcile latency by controller and result. |
| `tamoss_reconcile_errors_total` | Counts reconcile errors by controller. |

Metrics mirror status. If an alert fires, inspect the same resource with
`kubectl describe` and use the matching condition reason as the starting point.

Operator alerts:

| Alert | First check |
| --- | --- |
| `TAMOSSReconcileErrors` | Operator logs and recent Events for the named controller. |
| `TAMOSSResourceDegraded` | `kubectl describe tamoss` or `storagebackend` and the `Degraded` reason. |
| `TAMOSSResourceNotReady` | `Ready` condition reason, dependent pods, and provider resources. |
| `TAMOSSProviderNotReady` | `BackendsReady`, `IdentityReady`, and provider ownership status. |
| `TAMOSSSchemaMigrationFailureRate` | `status.schemaMigration`, migration Jobs, and schema retry state. |
| `TAMOSSSchemaVersionDrift` | Operator rollout and image versions before instance upgrades. |
| `TAMOSSReconcilePhaseShift` | Recent Events and conditions for repeated degraded/progressing reconciles. |

Secret rotation uses file-backed runtime inputs for TAMOSS-controlled API and
worker processes. See [Secret Rotation](secret-rotation.md).

## Scaling

For durable scaling, edit the environment overlay that owns the `Tamoss` CR,
then reapply it:

```bash
$EDITOR deploy/environments/my-prod/tamoss-patch.yaml
task env:apply ENV=my-prod KUBECONFIG="$KUBECONFIG"
task env:wait ENV=my-prod KUBECONFIG="$KUBECONFIG"
```

For a short investigation, patch the live `Tamoss` CR and then copy the chosen
state back into source control if it should persist:

```bash
kubectl --kubeconfig "$KUBECONFIG" -n "$TAMOSS_NAMESPACE" patch tamoss "$TAMOSS_NAME" \
  --type=merge -p '{"spec":{"api":{"replicaCount":3},"worker":{"replicaCount":3},"ui":{"replicaCount":2}}}'
```

Manual `kubectl scale deployment ...` is temporary. The operator reconciles
managed Deployments back to the CR.

## Pausing Reconciliation

Pause only for short investigations:

```bash
kubectl --kubeconfig "$KUBECONFIG" -n "$TAMOSS_NAMESPACE" patch tamoss "$TAMOSS_NAME" \
  --type=merge -p '{"spec":{"paused":true}}'
```

Resume:

```bash
kubectl --kubeconfig "$KUBECONFIG" -n "$TAMOSS_NAMESPACE" patch tamoss "$TAMOSS_NAME" \
  --type=merge -p '{"spec":{"paused":false}}'
```

While paused, the operator updates status but avoids write actions. Do not leave
production instances paused longer than needed.

## Generated API Token

When `.spec.secrets.apiToken.generate=true`, the operator creates:

```text
Secret/<fullname>-api-token
```

The local Kind profile uses `tams-api-token`.

Rotate an operator-generated token by changing the rotation annotation value:

```bash
kubectl --kubeconfig "$KUBECONFIG" -n "$TAMOSS_NAMESPACE" annotate tamoss "$TAMOSS_NAME" \
  tamoss.livewyer.io/api-token-rotate="$(date -Iseconds)" --overwrite
```

Rotation is only supported for operator-generated tokens. If
`.spec.secrets.apiToken.token` is supplied directly, change that spec value in
source control instead. After rotation, retrieve the new Secret value and update
clients that use the API token.

## Drift

The operator renders canonical resources from the CR on every reconcile. It
corrects manual edits to managed Deployments, Services, Ingresses, Jobs,
ConfigMaps, and Secrets unless the instance is paused.
