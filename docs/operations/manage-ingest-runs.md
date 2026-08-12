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
export INGEST_RUN=example-run
```

## Enable an Ingest Source

Production profiles default to `Disabled`. Choose `PublicHTTPS` for unrestricted
public HTTPS assets or define reusable named sources and use `Restricted`. Apply
the `Tamoss` change through the normal environment workflow. The
[reference examples](../reference/ingestrun-cr.md#public-https-example) show the
exact shapes.

Do not put a signed URL in a run or credentials in a source definition. Put any
credentials in the source-owned Secret described by the reference.

## Create a Run with Kubernetes

Create a run for one selector:

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
  input:
    kind: HTTP
    uri: https://media.example.com/programmes/example.mp4
  profile: essence-segments@1
  sizeClass: standard
  options:
    maxInputs: 1
  output:
    flowMetadata:
      label: Example programme
      description: Programme ingest requested by media operations
      tags:
        editorial_purpose:
          - programme
EOF
```

Confirm that the request was accepted and acquired a phase:

```bash
kubectl -n "$TAMOSS_NAMESPACE" get ingestrun "$INGEST_RUN" -w
```

The output block is optional. It is available only when `maxInputs` is exactly
one. TAMSin applies this metadata to the root and member Flows in the generated
graph. The `_tamsin_` tag prefix is reserved.

The Console deliberately has no POST endpoint or create form. Use `kubectl`,
GitOps, or deployment automation with explicit Kubernetes RBAC.

## Inspect Run History in the Console

1. Open the TAMOSS UI and select **Ingest runs**.
2. Filter by phase when investigating active, failed, or completed work.
3. Open a run to inspect its target, profile, output intent, attempt, progress,
   conditions, Job reference, verified result metadata, and links to resulting
   Flows.
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
  -o jsonpath='{.status.phase}{"\n"}{.status.progress}{"\n"}{.status.output}{"\n"}{.status.conditions}{"\n"}'
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
| `InputPolicyRejected` | Check source-policy mode, source name, origin or bucket, prefixes, and source-owned credential Secret. |
| `IngestStorageBackendNotReady` | Restore the selected media backend or choose a Ready backend belonging to the same instance. |
| `IngestFlowProfileNotFound` | Create the referenced `FlowProfile` in the run namespace or correct its name. |
| `IngestFlowProfileNotReady` | Wait for current `FlowProfile` registration and inspect its conditions. |
| `IngestFlowProfileTargetMismatch` | Select a `FlowProfile` associated with the run's target `Tamoss`. |
| `IngestFlowProfileFormatMismatch` | Match the assignment's `format` to the resolved Profile format. |
| `TamsinRuntimeUnavailable` or `TamsinImageNotImmutable` | Check the operator's configured [TAMSin](https://github.com/livewyer-ops/tamsin) image and release metadata. |
| `IngestProtocolInvalid` | Inspect the operator-owned Job and TAMSin logs; the terminal event stream failed protocol or exit-code validation. |
| `IngestResultPending` | Wait for the terminal Pod log to become readable. |
| `IngestResultUnavailable` | The terminal event stream remained unavailable for 15 minutes; retain the run and investigate cluster log retention. |
| `IngestJobMissing` | Treat the outcome as unknown and create a deliberate retry only after reviewing possible side effects. |

Verify the target mode and named source metadata without printing Secret
values:

```bash
kubectl -n "$TAMOSS_NAMESPACE" get tamoss \
  "$(kubectl -n "$TAMOSS_NAMESPACE" get ingestrun "$INGEST_RUN" -o jsonpath='{.spec.tamossRef.name}')" \
  -o jsonpath='{.status.phase}{"\n"}{.spec.ingest.sourcePolicy.mode}{"\n"}{.spec.ingest.sources[*].name}{"\n"}'
```

For general instance and workload failures, continue with
[Troubleshooting](troubleshooting.md).
