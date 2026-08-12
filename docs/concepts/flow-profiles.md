# Flow Profiles

A TAMS Flow Profile is an immutable, reusable set of technical Flow metadata.
It lets clients create multiple compatible Flows from one named technical
definition while each Flow keeps its own common metadata and identity.

Flow Profiles are distinct from TAMOSS deployment profiles such as
`local-kind` and `multi-server`.

## Eager Expansion

TAMOSS expands a Profile when the Flow is written. The Flow row stores the
Profile UUID and a complete copy of its technical metadata; a later Flow read
does not join the Flow to the Profile table.

This model provides:

- one database lookup for a normal Flow read;
- a self-contained Flow record suitable for webhook and API projection;
- stable historical meaning because Profiles are immutable; and
- efficient filtering by the retained `profile_id`.

The UUID remains a provenance link, not a live inheritance mechanism. Changing
or deleting Profile data behind existing Flows would make the same UUID mean
different technical metadata, so the public Profile API does not provide
update or delete operations.

## Kubernetes Ownership

`FlowProfile` provides a declarative Kubernetes owner for a TAMS Profile. It is
a namespaced resource associated with one `Tamoss` in the same namespace. The
operator validates its definition with the TAMOSS application, creates or
adopts the exact immutable Profile UUID, and publishes the resolved identity in
status.

If `spec.id` is omitted, the operator derives a stable UUID from the Kubernetes
namespace and resource name. Supplying an ID is useful when adopting an
existing Profile. Adoption succeeds only when the stored Profile definition is
identical; an ID collision with different metadata fails closed.

The Kubernetes specification is immutable. Create a differently named
`FlowProfile` for a changed technical definition. This preserves the TAMS rule
that one Profile UUID always describes one technical identity.

Deletion is also deliberate. `FlowProfile` uses the standard confirmation
annotation, then an internal application command removes the TAMS Profile only
when no Flow references it. A referenced Profile remains in `Deleting` with a
`DeletionBlocked=True` condition until those references are unlinked or
deleted. This internal lifecycle does not expose a public TAMS Profile DELETE
operation.

## Ownership of Metadata

`flow_metadata` contains technical Flow fields and compatible extensions.
Common and server-owned fields such as Flow ID, Source ID, label, description,
status, tags, timestamps, and collections remain owned by the Flow and are
rejected inside a Profile.

When creating a Profile-backed Flow, the client supplies `profile_id` and the
Flow's common metadata. It must not override technical fields supplied by the
Profile. Once linked:

- common Flow metadata may still change;
- Profile-owned technical fields and `avg_bit_rate` cannot be changed through
  Flow operations;
- a direct Flow cannot acquire a Profile later; and
- a linked Flow cannot be redirected to another Profile.

## Explicit Unlinking

A linked Flow can become a direct Flow by sending `profile_id: ""` with a
complete valid replacement technical definition. TAMOSS removes the Profile
association and strips fields inherited from the old Profile before storing
the replacement.

Unlinking is explicit because omission means "retain the existing
association" during an update. A newly created or already direct Flow cannot
use the empty-string sentinel.

## User Interface Boundary

The UI provides read-only Profile listing and detail, links Profile-backed
Flows to their Profile, and can filter Flows by `profile_id`. Profile creation
remains a TAMS API operation with write scope; browser forward-auth sessions are
read-only. The UI also displays `FlowProfile` provenance recorded on an
`IngestRun`, but does not create, update, retry, or delete these resources.

Use the [API reference](../reference/api.md) for the contract and interactive
OpenAPI documentation. Use [Manage Flow Profiles](../operations/manage-flow-profiles.md)
for the Kubernetes workflow, [FlowProfile CR](../reference/flowprofile-cr.md)
for fields, and [Deployment Profiles](profiles.md) for installation shapes.
