# FlowProfile CR Reference

`FlowProfile` declaratively registers one immutable TAMS Flow Profile in a
same-namespace `Tamoss` instance.

Group: `tamoss.livewyer.io`

Version: `v1alpha1`

Kind: `FlowProfile`

Scope: `Namespaced`

Short name: `fp`

## Example

```yaml
apiVersion: tamoss.livewyer.io/v1alpha1
kind: FlowProfile
metadata:
  name: hd-avc
  namespace: tams
spec:
  tamossRef:
    name: tamoss-media
  label: HD AVC
  description: 1080p AVC mezzanine Profile
  tags:
    tier: mezzanine
    editorial_purpose:
      - programme
      - trailer
  flowMetadata:
    format: urn:x-nmos:format:video
    codec: video/h264
    container: video/mp4
    avg_bit_rate: 8000000
    segment_duration:
      numerator: 2
      denominator: 1
    essence_parameters:
      frame_rate:
        numerator: 25
        denominator: 1
      frame_width: 1920
      frame_height: 1080
```

The operator derives a deterministic UUID when `spec.id` is absent. Use an
explicit canonical UUID only when identity must be shared with another system
or an identical Profile is being adopted:

```yaml
spec:
  id: 60d9df18-6d9d-4b86-84bf-d1dcf14b3a28
```

## Spec Fields

| Field | Purpose |
| --- | --- |
| `.spec.tamossRef.name` | Target `Tamoss` in the same namespace. Required and immutable. |
| `.spec.id` | Optional canonical TAMS Profile UUID. Omission derives a stable UUID from namespace/name. Immutable. |
| `.spec.label` | Optional human-readable Profile label. Immutable. |
| `.spec.description` | Optional Profile description. Immutable. |
| `.spec.tags` | Optional TAMS tags. Each value is either a string or an array of strings. Immutable. |
| `.spec.flowMetadata` | Required TAMS `flow_metadata` object. Use TAMS field names such as `avg_bit_rate` and `essence_parameters`. Immutable. |

`flowMetadata` accepts the four TAMS Profile formats: video, audio, image, and
data. TAMOSS performs the authoritative Profile validation before database
registration. Common or server-owned Flow fields such as `id`, `source_id`,
`label`, `description`, `tags`, `status`, and `profile_id` are not valid inside
`flowMetadata`.

The CRD preserves extension fields so stores can retain compatible TAMS
extensions. Invalid metadata sets the resource to `Degraded`; it is not written
to the database.

## Status Fields

| Field | Purpose |
| --- | --- |
| `.status.observedGeneration` | Resource generation observed by the controller. |
| `.status.phase` | `Pending`, `Progressing`, `Ready`, `Degraded`, or `Deleting`. |
| `.status.profileID` | Resolved immutable TAMS Profile UUID. |
| `.status.resolved.tamossName` | Target instance used for registration. |
| `.status.resolved.format` | Validated TAMS Flow format. |
| `.status.resolved.codec` | Validated codec when the Profile supplies one. |
| `.status.conditions` | `Ready`, `Registered`, and `DeletionBlocked` conditions with stable reasons. |

`Ready=True` means that the exact desired definition has been created or
adopted. A duplicate UUID owned by another `FlowProfile`, a different existing
TAMS definition, or invalid metadata produces `Degraded` rather than replacing
the Profile.

## Deletion Contract

Deletion requires:

```yaml
metadata:
  annotations:
    confirmation.tamoss.livewyer.io/deletion: "true"
```

After admission, the finaliser invokes the TAMOSS application against the
target database. The TAMS Profile is removed only if no Flow currently
references its UUID. Otherwise `DeletionBlocked=True` and the Kubernetes
resource remains terminating. Deleting the deterministic UUID owner is also
blocked while another live `FlowProfile` claims the same target UUID,
preventing one Kubernetes owner from removing another owner's Profile. A
losing duplicate can be deleted without touching the shared TAMS Profile.
Removing the finaliser manually can leave the TAMS Profile behind and is not a
supported deletion path.

See [Manage Flow Profiles](../operations/manage-flow-profiles.md) for commands
and [Flow Profiles](../concepts/flow-profiles.md) for the data model.
