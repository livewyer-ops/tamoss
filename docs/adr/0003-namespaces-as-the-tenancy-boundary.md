---
status: "accepted"
---
# Namespaces as the Tenancy Boundary

## Context and Problem Statement

More than one team or customer may need a TAMS store on the same cluster, and their media must not be visible to each other.

The TAMS specification has no tenant dimension.
There is no tenant identifier on a Source, Flow or Object, and the API has no end-user-scoped authorisation model to attach one to.
Any tenancy TAMOSS offers therefore has to come from outside the API.

## Considered Options

* Option 1: Add a tenant dimension to the API and serve many tenants from one instance
* Option 2: One instance per tenant, isolated by Kubernetes namespace
* Option 3: One cluster per tenant

## Decision Outcome

Chosen Option 2: One instance per tenant, isolated by Kubernetes namespace.

A single cluster-wide operator reconciles many namespaced `Tamoss` resources against a shared platform install.
`docs/concepts/tenancy.md` is the full treatment.

**Confidence:** High.
The specification has no tenant dimension, so every alternative adds a concept it does not have.

**Reevaluate if:** the TAMS specification gains a tenant concept, or per-tenant instance cost dominates for a deployment with many small tenants.

### Consequences

* The boundary is enforced by Kubernetes, through RBAC, NetworkPolicy, quota, and deletion semantics, rather than by application code that would have to be correct on every path.
* Per-tenant cost is one full instance, not one row. Many small tenants are expensive.
* Cross-instance concerns have no home. Hostname admission is the first to bite: the operator keeps no cluster-wide hostname registry, so a duplicate is reported as a failed condition after the resource is accepted rather than rejected at apply time. The operator's own scale ceiling at high instance counts is also unestablished.
* Anything a tenant must not see has to be absent from their namespace, which constrains what shared platform services may hold.

## Pros and Cons of the Options

### Option 1: A tenant dimension in the API

* Good, because many tenants share one deployment, so per-tenant cost is small
* Good, because cross-tenant operations become possible
* Bad, because it adds a concept the TAMS specification does not have, so our API would no longer be the specification's API
* Bad, because isolation would rest on application code being correct on every query path, with no enforcement underneath it
* Bad, because there is no end-user-scoped authorisation model to attach a tenant to, so it would have to be invented first

### Option 2: One instance per namespace

* Good, because Kubernetes enforces the boundary rather than application code
* Good, because it adds nothing to the API, which stays exactly the specification's
* Good, because namespace deletion is a complete and well-understood tenant teardown
* Bad, because per-tenant cost is a full instance
* Bad, because cluster-wide concerns such as hostname admission fall between instances

### Option 3: One cluster per tenant

* Good, because isolation is as strong as it can be
* Neutral, because it is the right answer where a tenant requires it, and remains available
* Bad, because per-tenant cost rises again, now including the platform components
* Bad, because it makes routine operations a fleet problem rather than a cluster problem
