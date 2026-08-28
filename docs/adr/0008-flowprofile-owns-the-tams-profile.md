---
status: "accepted"
---
# A Kubernetes Resource Owns Each TAMS Flow Profile

## Context and Problem Statement

TAMS 8.2 adds Flow Profiles, an immutable and reusable set of technical Flow metadata addressed by UUID and served from `/service/profiles` (upstream `ADR0047`).
Something has to put Profiles into a store before a client can create a Flow from one.

The specification makes that harder than it looks, because a Profile is immutable and the public API offers no update or delete.
A Profile created by an API call can never be corrected or withdrawn through the API, so an accident is permanent.
Everything else that defines an instance is declared in a resource and converged; Profiles arriving by imperative call would be the exception, and the one exception that cannot be undone.

## Considered Options

* Option 1: Profiles are created through the TAMS API, like every other TAMS object
* Option 2: The operator writes Profiles into the database directly
* Option 3: A `FlowProfile` resource, reconciled into the store through a Job that calls the application

## Decision Outcome

Chosen Option 3: A `FlowProfile` resource, reconciled into the store through a Job that calls the application.

`flowprofile_controller.go` renders `flowProfileRegistrationJob`, which runs `tamoss-profile ensure` against the instance rather than reaching for the database, so the operator gains a Profile without gaining a database client and [ADR0005](./0005-kubernetes-agnostic-api.md) is not weakened to get one.
Immutability is enforced at admission by CEL on every field of the spec (`operator/api/v1alpha1/flowprofile_types.go`), which matches the specification's rule that one Profile UUID always describes one technical identity.
Omitting `spec.id` derives a stable UUID from namespace and name, so a Profile has an identity before anything has run, and supplying one adopts an existing Profile only when the stored definition is identical.
Deletion runs the same way and fails closed, holding the resource in `Deleting` with `DeletionBlocked=True` while any Flow still references the Profile.

**Confidence:** High.
The specification's own immutability rule is what makes a declarative, converged owner the natural fit, and adoption and deletion both fail closed rather than guessing.

**Reevaluate if:** Profiles need to be created by clients at runtime rather than declared by an operator, or the registration Job's latency becomes a problem for instances holding many Profiles.

### Consequences

* Profiles are declarable alongside everything else, so an instance and the Profiles it serves can be one GitOps commit rather than an apply followed by a curl.
* A Profile now has two possible authors, `kubectl` and the TAMS API, and the resource is authoritative only for the Profiles it created. Nothing prevents a client from creating a Profile the cluster has no record of.
* Correcting a Profile is not an edit. Because both the specification and the CRD forbid it, the answer is always a differently named resource, and the old Profile stays until nothing references it.
* TAMOSS holds a Profile deletion capability that the TAMS API does not expose, reachable only through the operator's own Job. That is a deliberate asymmetry, and it means a private command surface exists that conformance testing does not cover.
* Registration costs a Job per Profile rather than a row insert. For a handful of Profiles this is invisible; it is the first thing to measure if an instance declares many.
* It extends [ADR0006](./0006-operator-owned-ingest-jobs.md) beyond media work. The Job pattern is now how the operator reaches into the application generally, not only how it runs TAMSin.

## Pros and Cons of the Options

### Option 1: Profiles created through the TAMS API

* Good, because it adds no resource, no controller, and no Job
* Good, because it is exactly the specification's own interface, with nothing local layered on top
* Neutral, because it remains available and cannot be prevented
* Bad, because a mistaken Profile is permanent, since the API has neither update nor delete
* Bad, because the Profiles an instance serves would not be visible in the resource that defines the instance
* Bad, because it has no convergence story, so a rebuilt store does not come back with the Profiles it had

### Option 2: The operator writes to the database

* Good, because it is a single insert with no Job and no intermediate CLI
* Good, because reconciliation could confirm the stored state directly
* Bad, because it gives the operator a database client and a second writer to a schema the application owns, so a migration would have to keep both in step
* Bad, because Profile validation would then exist twice, in the application and in the operator
* Bad, because it puts credentials for the index in a cluster-wide component

### Option 3: A `FlowProfile` resource reconciled through a Job

* Good, because the operator never learns the database, and validation stays in the application that owns it
* Good, because immutability and adoption are enforced at admission, so a conflicting definition fails at apply time
* Good, because deletion can be blocked on live references, which the public API could not express
* Bad, because it is a Job per registration rather than a write
* Bad, because a Profile can still be created out of band through the API, so the resource is not a complete inventory
