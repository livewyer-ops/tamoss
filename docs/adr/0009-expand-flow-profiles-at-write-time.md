---
status: "accepted"
---
# Expand Flow Profile Metadata Into the Flow at Write Time

## Context and Problem Statement

A Profile-backed Flow takes its technical metadata from a Profile that many Flows may share.
The store therefore holds the same relationship twice over: the Profile row that defines the metadata, and every Flow that was created from it.

Reading a Flow is the hottest metadata path in a TAMS store.
Flows are listed, filtered, projected into webhook payloads, and read again on every segment operation, so whatever a Flow read costs is paid constantly.
Where the Profile's fields are resolved decides that cost, and it decides what the Profile API is allowed to offer, because a Profile that can change underneath existing Flows means the same UUID describing different technical metadata at different times.

## Considered Options

* Option 1: Store `profile_id` and join the Profile on every Flow read
* Option 2: Copy the Profile's metadata into the Flow record at write time, keeping `profile_id` as provenance
* Option 3: Join at read, with a cache in front of the Profile table

## Decision Outcome

Chosen Option 2: Copy the Profile's metadata into the Flow record at write time, keeping `profile_id` as provenance.

`tamoss_flows.record` holds the complete resolved Flow document, and `profile_id` sits beside it as a bare UUID column with no foreign key to `tamoss_profiles` (`src/app/tamoss/db/migrations/assets/schema.sql`).
`idx_tamoss_flows_profile_id_id` keeps filtering by Profile cheap without making the read depend on the Profile table existing.
`docs/concepts/flow-profiles.md` is the full treatment and states the consequence plainly: the UUID "remains a provenance link, not a live inheritance mechanism".

**Confidence:** High.
Immutability is the specification's rule rather than ours, so the correctness condition this decision depends on is not one we have to maintain.

**Reevaluate if:** Profiles gain mutable fields upstream, or the duplicated metadata becomes a measurable share of index size.

### Consequences

* A Flow read is one lookup and touches one table, so the hottest path does not get slower as the number of Profiles grows.
* A Flow record is self-contained, which is what lets webhook payloads and API responses be projected without reaching for a second row.
* It forecloses Profile mutation. The public Profile API cannot offer update or delete without making a UUID mean two different things over time, so a decision about read cost became a decision about the API surface.
* A mistake in a Profile cannot be corrected for the Flows already created from it. The fix is a new Profile and new Flows, and the old metadata stays exactly as it was written.
* Technical metadata is duplicated once per Flow rather than stored once per Profile. This is proportional to Flow count, and it is the cost this decision trades read latency for.
* Reversing it is a migration over every Flow row, not a code change, because the expanded copies would have to be reduced back to references.

## Pros and Cons of the Options

### Option 1: Join on every read

* Good, because the metadata is stored once, and a Profile has exactly one representation
* Good, because a Profile correction would reach existing Flows immediately
* Neutral, because the join itself is cheap on an indexed primary key
* Bad, because the hottest path in the store gains a second table, and listings turn one query into a join per page
* Bad, because a Flow could no longer be projected into a webhook payload without a second read
* Bad, because it makes Profile mutation look safe when the specification's immutability rule says it is not, so the API would be one careless endpoint away from breaking historical meaning

### Option 2: Expand at write time

* Good, because Flow reads stay single-table and Flow records stay self-contained
* Good, because historical meaning is stable by construction rather than by convention
* Good, because `profile_id` still supports filtering and provenance without being load-bearing
* Bad, because technical metadata is duplicated per Flow
* Bad, because no Profile correction can reach existing Flows
* Bad, because reversing the decision is a data migration

### Option 3: Join with a cache

* Good, because it keeps single storage and recovers most of the read cost
* Neutral, because Profile immutability makes the cache trivially safe to hold
* Bad, because it adds an invalidation path and a memory budget to solve a problem Option 2 removes outright
* Bad, because it leaves Flow projection needing two lookups on a cold cache, so the worst case is unchanged
* Bad, because it still implies Profiles could change, carrying Option 1's pressure on the API surface
