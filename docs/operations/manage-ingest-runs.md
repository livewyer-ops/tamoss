# Manage Ingest Runs

Use this guide to inspect durable ingest history, follow the operator-owned
workload, and cancel an active run without mutating its Kubernetes Job.

For the resource model, read [Ingest Runs](../concepts/ingest-runs.md). For
field-level lookup, use the [IngestRun CR reference](../reference/ingestrun-cr.md).

## Prerequisites

- A running `Tamoss` instance with Console enabled.
- `viewer` access to inspect runs or `operator` access to cancel through the
  Console.
- For `kubectl` commands, permission to read `IngestRun` resources in the
  instance namespace. Patching cancellation requires explicit update or patch
  permission on those resources.

Set the namespace and run name used by the examples:

```bash
export TAMOSS_NAMESPACE=tams
export TAMOSS_NAME=tamoss-kind
export INGEST_INPUT=example-programme
export INGEST_RUN=example-run
```

## Create a Run from an Approved Input

First add the HTTPS location to
`.spec.ingest.approvedInputs` in the source-controlled `Tamoss` environment and
apply it through the normal environment workflow. The
[reference example](../reference/ingestrun-cr.md#approved-input-and-run-example)
shows the exact shape. Do not put a signed URL or credentials in the approval.

Create a run that names only the approved ID:

```bash
kubectl apply -f - <<EOF
apiVersion: tamoss.livewyer.io/v1alpha1
kind: IngestRun
metadata:
  name: ${INGEST_RUN}
  namespace: ${TAMOSS_NAMESPACE}
spec:
  tamossRef:
    name: ${TAMOSS_NAME}
  inputRef:
    kind: ApprovedHTTP
    id: ${INGEST_INPUT}
  profile: editorial@1
  sizeClass: standard
EOF
```

Confirm that the request was accepted and acquired a phase:

```bash
kubectl -n "$TAMOSS_NAMESPACE" get ingestrun "$INGEST_RUN" -w
```

The current Console does not expose a create command. Kubernetes creation
therefore requires an explicitly authorised administrator or deployment
automation.

## Inspect Run History in the Console

1. Open the TAMOSS UI and select **Ingest runs**.
2. Filter by phase when investigating active, failed, or completed work.
3. Open a run to inspect its target, profile, attempt, progress, conditions,
   Job reference, and verified result metadata.
4. Follow the next-page control until it is absent. The list is cursor-paged
   and does not promise a total count.

The Console deliberately omits raw Kubernetes objects, logs, Secret data, and
private result keys.

## Inspect a Run with kubectl

List the namespace's runs with their target and phase:

```bash
kubectl -n "$TAMOSS_NAMESPACE" get ingestruns \
  -o custom-columns='NAME:.metadata.name,TAMOSS:.spec.tamossRef.name,PROFILE:.spec.profile,PHASE:.status.phase,JOB:.status.jobRef.name'
```

Inspect the selected run:

```bash
kubectl -n "$TAMOSS_NAMESPACE" describe ingestrun "$INGEST_RUN"
kubectl -n "$TAMOSS_NAMESPACE" get ingestrun "$INGEST_RUN" \
  -o jsonpath='{.status.phase}{"\n"}{.status.progress}{"\n"}{.status.conditions}{"\n"}'
```

If `status.jobRef.name` is present, check the operator-owned Job without
editing it:

```bash
JOB_NAME="$(kubectl -n "$TAMOSS_NAMESPACE" get ingestrun "$INGEST_RUN" \
  -o jsonpath='{.status.jobRef.name}')"
kubectl -n "$TAMOSS_NAMESPACE" get job "$JOB_NAME"
```

Do not delete, patch, or recreate this Job. The operator owns its template and
lifecycle. Removing it can end the run as `Failed` with reason
`IngestJobMissing` rather than causing a retry.

## Cancel an Active Run

In the Console detail view, select **Cancel run** and confirm. The action is
shown only when the run is cancellable and the current session has the
`operator` role.

A Kubernetes administrator can request the same one-way state change directly:

```bash
kubectl -n "$TAMOSS_NAMESPACE" patch ingestrun "$INGEST_RUN" \
  --type=merge \
  -p '{"spec":{"desiredState":"Cancelled"}}'
```

Wait for workload termination:

```bash
kubectl -n "$TAMOSS_NAMESPACE" wait \
  --for=jsonpath='{.status.phase}'=Cancelled \
  "ingestrun/$INGEST_RUN" --timeout=5m
```

Cancellation is irreversible. Do not patch `desiredState` back to `Running` or
modify status. A retry must be a new resource linked to the terminal parent;
the current Console does not provide a create or retry command.

## Diagnose a Pending or Failed Run

Read `.status.conditions` first. Common reasons include:

| Reason | Action |
| --- | --- |
| `TamossNotReady` | Restore the referenced instance to `Ready=True`. |
| `InputResolutionFailed` | Confirm `inputRef` exactly matches an approved input on the target `Tamoss`. |
| `CredentialProfileResolverUnavailable` | Remove `credentialProfileRef`; credential profiles are not enabled. |
| `IngestStorageBackendNotReady` | Restore the selected media backend or choose a Ready backend belonging to the same instance. |
| `TamsinRuntimeUnavailable` or `TamsinImageNotImmutable` | Check the operator's configured Tamsin image and release metadata. |
| `IngestJobMissing` | Treat the outcome as unknown and create a deliberate retry only after reviewing possible side effects. |

Verify the target configuration and approved input IDs without printing Secret
values:

```bash
kubectl -n "$TAMOSS_NAMESPACE" get tamoss \
  "$(kubectl -n "$TAMOSS_NAMESPACE" get ingestrun "$INGEST_RUN" -o jsonpath='{.spec.tamossRef.name}')" \
  -o jsonpath='{.status.phase}{"\n"}{.spec.ingest.approvedInputs[*].id}{"\n"}'
```

For general instance and workload failures, continue with
[Troubleshooting](troubleshooting.md).
