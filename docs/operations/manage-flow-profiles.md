# Manage Flow Profiles

Use `FlowProfile` to register immutable TAMS technical definitions through the
Kubernetes API and refer to them from `IngestRun` resources.

For the model, read [Flow Profiles](../concepts/flow-profiles.md). For every
field and condition, use the [FlowProfile CR reference](../reference/flowprofile-cr.md).

## Prerequisites

- A `Tamoss` instance with its schema Ready.
- Permission to create `FlowProfile` resources in the instance namespace.
- A complete technical definition valid for the chosen TAMS format.
- An authenticated TAMS API client for checking existing Flow references.

Set the example names:

```bash
export TAMOSS_NAMESPACE=tams
export TAMOSS_NAME=tamoss-media
export FLOW_PROFILE=hd-avc
export TAMOSS_API=https://api.tamoss.example.com
```

## Register a Profile

Apply an immutable definition:

```bash
kubectl apply -f - <<EOF
apiVersion: tamoss.livewyer.io/v1alpha1
kind: FlowProfile
metadata:
  name: ${FLOW_PROFILE}
  namespace: ${TAMOSS_NAMESPACE}
spec:
  tamossRef:
    name: ${TAMOSS_NAME}
  label: HD AVC
  flowMetadata:
    format: urn:x-nmos:format:video
    codec: video/h264
    container: video/mp4
    essence_parameters:
      frame_rate:
        numerator: 25
        denominator: 1
      frame_width: 1920
      frame_height: 1080
EOF
```

Wait for exact registration or adoption:

```bash
kubectl -n "$TAMOSS_NAMESPACE" wait \
  --for=condition=Ready "flowprofile/$FLOW_PROFILE" --timeout=5m
kubectl -n "$TAMOSS_NAMESPACE" get flowprofile "$FLOW_PROFILE" \
  -o custom-columns='NAME:.metadata.name,ID:.status.profileID,FORMAT:.status.resolved.format,READY:.status.conditions[?(@.type=="Ready")].status'
```

The UI is read-only for Profile and ingest configuration. Use Kubernetes RBAC,
GitOps, or deployment automation to create `FlowProfile` resources; there is no
browser POST path.

## Assign the Profile to Ingest

An `IngestRun` can select the Kubernetes resource instead of copying its UUID:

```yaml
spec:
  options:
    tamsFlowProfiles:
      - format: video
        index: 0
        profileRef:
          name: hd-avc
```

The run remains `Pending` until the reference is Ready, belongs to the same
target `Tamoss`, and has the requested essence format. Before creating the
[TAMSin](https://github.com/livewyer-ops/tamsin) Job, the operator snapshots the
resolved UUID into `status.resolvedTamsFlowProfiles`. The Console shows both the
resource name and resolved UUID.

Use `profileID` instead of `profileRef` only for a Profile managed outside
Kubernetes. Each assignment must set exactly one of the two fields.

## Replace a Definition

The `FlowProfile` spec is immutable. To change technical metadata:

1. Create a new resource with a new name or explicit UUID.
2. Wait for `Ready=True`.
3. Point new `IngestRun` resources at the new name.
4. Unlink or remove Flows using the old Profile when operationally appropriate.
5. Delete the old resource using the confirmed procedure below.

Do not reuse a UUID for different metadata. The operator rejects that conflict
instead of changing existing Flow provenance.

## Delete a Profile

Confirm and request deletion:

```bash
kubectl -n "$TAMOSS_NAMESPACE" annotate flowprofile "$FLOW_PROFILE" \
  confirmation.tamoss.livewyer.io/deletion=true
kubectl -n "$TAMOSS_NAMESPACE" delete flowprofile "$FLOW_PROFILE" --wait=false
```

Watch the finaliser:

```bash
kubectl -n "$TAMOSS_NAMESPACE" get flowprofile "$FLOW_PROFILE" -w
```

If any Flow still references the Profile, the resource remains terminating and
reports `DeletionBlocked=True` with reason `ProfileInUse`. Find the references:

```bash
PROFILE_ID="$(kubectl -n "$TAMOSS_NAMESPACE" get flowprofile "$FLOW_PROFILE" -o jsonpath='{.status.profileID}')"
curl --fail --get "$TAMOSS_API/flows" --data-urlencode "profile_id=$PROFILE_ID"
```

Unlinking a Flow requires a complete replacement technical definition and
`profile_id: ""`; deleting a Flow follows the normal TAMS deletion workflow.
The finaliser retries and removes the Profile only after the reference count is
zero.

`WaitingForSchema` means the target schema is unavailable or being upgraded.
The finaliser keeps the registered Profile until it can check those references.

`ProfileClaimedByFlowProfile` means another live resource claims the same UUID
for the same target. Delete the `Degraded` duplicate claim first; the operator
will release that losing claim without touching the Profile, then allow its
deterministic owner to continue.

Do not remove the finaliser manually. If the target `Tamoss` has already been
deleted, cleanup is best effort and the database Profile can remain for later
administrative recovery.
