---
status: "accepted"
---
# A Separate Console API Reads Kubernetes for the UI

## Context and Problem Statement

The UI has to show things only Kubernetes knows.
Ingest run history and phase, workload readiness, and the events behind a failure all live in the cluster, and an operator looking at a stuck run wants to cancel it from the same screen.

[ADR0005](./0005-kubernetes-agnostic-api.md) put the application on the far side of that boundary deliberately: no TAMOSS application workload calls the Kubernetes API, and status is the operator's to derive.
It records the cost in its own consequences, that the application "cannot report what it learns back to Kubernetes".
The reverse gap is the one that bites here, because the operator projects configuration into pods at restart and has no path for handing runtime state back out to a browser.

Something has to hold Kubernetes credentials for the UI to be more than a TAMS client.
The question is which workload, and how much it is allowed to see.

## Considered Options

* Option 1: Grant the existing API a ServiceAccount and let it read Kubernetes directly
* Option 2: Have the operator write the state the UI needs into the database, and serve it from the existing API
* Option 3: A separate console workload holding narrowly scoped Kubernetes access

## Decision Outcome

Chosen Option 3: A separate console workload holding narrowly scoped Kubernetes access.

`operator/cmd/console-api/main.go` is a distinct Go binary with its own image, not a capability added to the Python application.
`ReadOnlyPolicyRules` (`operator/internal/consoleapi/rbac.go`) is a namespaced Role rather than a ClusterRole, narrows `tamosses` to the single instance by `resourceNames`, and states its own exclusions: "intentionally excludes Secrets, logs, exec, proxy, and subresources".
The only mutating verb is the `patch` that backs ingest run cancellation (`command_contract.go`).
`ConsoleComponentSpec` (`operator/api/v1alpha1/tamoss_spec_types.go`) defaults `enabled` to false, so an install carries the component only when it asks for it.

**Confidence:** Medium.
The separation is sound and the Role is tightly drawn, but the surface has served one feature, so how far the read set grows is not yet known.

**Reevaluate if:** the console needs cluster-scoped reads, Secrets, or pod logs, or a second consumer needs the same projection and the console becomes a general gateway.

### Consequences

* [ADR0005](./0005-kubernetes-agnostic-api.md) survives intact where it matters most. The API and worker still hold no cluster credentials, so the workloads handling media requests are unchanged in what a compromise would yield.
* There is now a third long-running workload, with its own image, release cadence, probes, and scaling story, none of which the API and worker share.
* The console does hold Kubernetes credentials. The blast radius is bounded by the Role rather than by the boundary being absent, which is a weaker guarantee than ADR0005 gives the API and a stronger one than a ClusterRole would.
* Authentication is a second model. Forward-auth headers with a shared secret and group-to-role bindings (`consoleapi/auth.go`) have nothing in common with the TAMS API's scopes, so an operator configures identity twice.
* The UI now has two classes of feature. When the console is disabled the Kubernetes-derived ones fail with an explicit 503 rather than degrading, so "the UI works" is no longer a single statement.
* Read projection is deliberately lossy, omitting EndpointSlice addresses, target references, and artefact locators. Anything the UI later needs must be added to both the Role and the projection.

## Pros and Cons of the Options

### Option 1: Kubernetes access in the existing API

* Good, because it adds no workload, no image, and no second authentication model
* Good, because the UI would talk to one endpoint for everything
* Bad, because it puts cluster credentials in the internet-facing workload, which is the specific outcome [ADR0005](./0005-kubernetes-agnostic-api.md) exists to prevent
* Bad, because the application could no longer be run or tested without a cluster, ending the fast local test suite
* Bad, because it splits responsibility for rendering an instance across the operator and the application again

### Option 2: The operator writes runtime state to the database

* Good, because no new workload is needed and the UI keeps one endpoint
* Good, because cluster credentials stay with the operator, where they already are
* Neutral, because the operator already watches everything the projection would need
* Bad, because it makes the operator a writer to the application's database, coupling two components that are otherwise joined only by projected configuration
* Bad, because live state would arrive at reconcile cadence, and a cancel button needs a request path rather than a table

### Option 3: A separate console workload

* Good, because the credential boundary moves to a component that can be omitted entirely, and is by default
* Good, because the permission contract is small enough to state in one function and assert in a test
* Good, because ingest control reaches the UI without the API learning what Kubernetes is
* Bad, because it is a third workload to build, release, secure, and document
* Bad, because identity is configured twice, once for TAMS and once for the console
* Bad, because UI capability now depends on whether an optional component is installed
