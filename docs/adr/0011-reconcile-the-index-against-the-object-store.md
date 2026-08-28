---
status: "proposed"
---
# Reconcile the Index Against the Object Store

## Context and Problem Statement

[ADR0001](./0001-media-never-transits-the-cluster.md) put the index and the media bytes in two systems that are backed up, restored and mutated independently.
`tamoss_media_objects.id` is simultaneously a PostgreSQL primary key and an object key in a bucket, and that pairing is the one relationship in the system the database cannot enforce.
Nothing in the tree inspects it.

The absence is in the abstraction rather than only in the call sites.
The `ObjectStorage` port (`src/app/tamoss/ports/object_storage.py`) exposes six methods, `build_put_request`, `build_get_urls_batch`, `object_metadata`, `copy`, `delete`, and `delete_batch`, none of which lists a bucket.
There is therefore no call to make.

The gap is one-directional.
TAMOSS can find rows no Flow references any more, through `idx_tamoss_media_objects_unreferenced` and the `tamoss_object_cleanups` queue, but it can never find a bucket key with no row at all.

Restore skew is the failure mode, and it breaks the pairing in both directions.
Restoring PostgreSQL to an earlier point than the bucket leaves objects with no row: invisible to the API, never collected, paid for indefinitely.
The restored database has no memory of the rows it lost, so the unreferenced-object machinery cannot help, because those objects are not unreferenced but unknown.
The reverse leaves rows referencing objects already deleted, which surfaces as a presigned URL resolving to a 404 at playback and, in a media archive, is discovered long after the fact.

A related weakness widens the reverse case.
The schema declares exactly one foreign key, `tamoss_segments.flow_id` to `tamoss_flows` (`src/app/tamoss/db/migrations/assets/schema.sql`).
`tamoss_segments.object_id` has none, so a Segment referencing a missing object row is prevented only by application code.
A restore that loses `tamoss_media_objects` rows while keeping Segment rows leaves Segments addressing objects with neither a row nor bytes, invisible to the unreferenced index and to any bucket walk alike.

## Considered Options

Direction of the check:

* Option 1a: Walk `tamoss_media_objects` and HEAD each object, finding rows without objects
* Option 1b: Add a list operation to the `ObjectStorage` port and walk the bucket, finding objects without rows
* Option 1c: Both

What the check does about what it finds:

* Option 2a: Report only
* Option 2b: Report, and delete confirmed orphans behind explicit confirmation

Whether to constrain the schema as well:

* Option 3a: Leave `tamoss_segments.object_id` unconstrained
* Option 3b: Add the missing foreign key

## Decision Outcome

Preferred options, not yet agreed:

* Option 1c, sequenced: ship 1a first, then 1b
* Option 2a first, with 2b as a later increment
* Option 3b, taken opportunistically rather than scheduled

The two directions cost very different amounts, which is why they should not land together.
Option 1a needs no new port surface at all: `object_metadata` already returns `None` on a 404, so the check is a walk over `tamoss_media_objects` issuing HEADs.
Option 1b requires adding a list operation to the port and implementing it in every adapter, because none exists today, and it widens a contract [ADR0001](./0001-media-never-transits-the-cluster.md) deliberately kept narrow.
Widening it for a control-plane reconciliation pass does not reintroduce the media path to the cluster, so the two are compatible, but the port should gain `list` and nothing more.

Report-only is shippable on its own and is worth shipping on its own.
Deleting confirmed orphans is destructive and must sit behind explicit confirmation, per the engineering standard on destructive actions, and an orphan report that a human acts on is a much smaller step than a sweeper that acts unattended.

Option 3b is cheap and prevents one class of the reverse case outright, but it cannot be assumed safe: existing instances may already hold Segment rows that would violate it, so it needs a migration that reports violations before it adds the constraint.

**Confidence:** Medium.
The direction is clear and the first increment is cheap, but the second widens a port contract in a way that has not been designed.

**Reevaluate if:** a storage adapter is added that cannot list a bucket efficiently, which would change the cost of Option 1b.

### Consequences

* The reconciliation pass is the first thing in TAMOSS that reads the whole bucket, so its cost scales with object count rather than with request rate. It belongs on a schedule an operator controls, not on the worker's poll loop.
* Adding `list` to the port means every current and future storage adapter must implement it, including any that cannot list efficiently.
* A report is only as good as the recovery advice attached to it. An orphan report with no documented next step will be ignored.
* Backup and restore verification gains something concrete to assert against, which the restore procedure currently asks an operator to sample by hand.

## Pros and Cons of the Options

### Option 1a: Walk the rows, HEAD each object

* Good, because it needs no change to the port and no adapter work
* Good, because it detects the case that surfaces to users as a broken playback URL
* Good, because it is the direction an end-to-end restore test needs in order to assert media survived
* Bad, because it cannot find objects that no row knows about, which is the direction that costs money silently

### Option 1b: Add `list` to the port and walk the bucket

* Good, because it is the only way to find unknown objects, which nothing today can detect
* Good, because it bounds storage cost, which is otherwise unbounded after a skewed restore
* Bad, because it widens a port contract kept deliberately narrow
* Bad, because every adapter must implement it, and listing a large bucket is expensive

### Option 2a: Report only

* Good, because it is safe to run anywhere, including against production, at any time
* Good, because it ships without needing a confirmation mechanism
* Bad, because remediation stays manual, and a report nobody acts on has no effect

### Option 2b: Report and delete behind confirmation

* Good, because it closes the loop rather than handing a human a list
* Bad, because a bug in the check becomes data loss rather than a wrong report
* Bad, because the stale-allocation sweeper is already a case of unattended deletion causing harm, and this would add a second

### Option 3a: Leave `object_id` unconstrained

* Good, because it changes nothing and risks no migration failure
* Bad, because the database continues not to enforce a relationship it could

### Option 3b: Add the foreign key

* Good, because one class of dangling reference becomes impossible rather than merely detected
* Good, because it is a small change with a clear meaning
* Bad, because existing instances may hold violating rows, so the migration must report before it constrains
