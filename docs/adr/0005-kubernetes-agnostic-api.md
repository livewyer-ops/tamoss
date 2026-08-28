---
status: "accepted"
---
# Keep the API Kubernetes-Agnostic

## Context and Problem Statement

The operator knows things the application needs: which storage backends exist, what credentials reach them, and what the instance is called.
Some of that lives in custom resources and some in Secrets.

The application could read those itself, or the operator could project them into a form the application understands without knowing what Kubernetes is.

## Considered Options

* Option 1: The API and worker read custom resources and Secrets through the Kubernetes API
* Option 2: The operator projects configuration into environment variables and mounted files

## Decision Outcome

Chosen Option 2: The operator projects configuration into environment variables and mounted files.

No application workload calls the Kubernetes API.
The operator derives a runtime credentials Secret per instance, and the API and worker pods read it as a mounted file (`storage_backend_credentials_file`, `src/app/tamoss/settings.py`).
Storage backend metadata lives in the database, not in custom resources the application reads.

**Confidence:** High.
The local test suite runs with no cluster at all, which is the clearest evidence the boundary holds.

**Reevaluate if:** operator-derived status proves too coarse to diagnose problems the application can see but cannot report.

### Consequences

* The TAMS implementation is portable and testable without a cluster, which is what makes the fast local test suite possible.
* A public-facing workload needs no Kubernetes RBAC, so a compromised API pod has no cluster credentials to steal.
* A non-Kubernetes runtime can supply the same file format and the application is unchanged.
* The application cannot report what it learns back to Kubernetes. Status and conditions are the operator's to derive, which is why operational state visible in the API is not automatically visible in `kubectl`.
* Configuration changes flow at pod restart rather than being watched.
* Safety gates the operator ought to own are reachable today only as environment variables through the free-form `spec.worker.env` map, rather than as typed fields with status visibility.

## Pros and Cons of the Options

### Option 1: The application reads Kubernetes directly

* Good, because configuration changes could be watched and applied without a restart
* Good, because the application could publish its own status and events
* Bad, because it requires granting cluster credentials to an internet-facing workload
* Bad, because the application could no longer be run or tested without a cluster
* Bad, because it splits responsibility for rendering an instance between the operator and the application

### Option 2: The operator projects configuration

* Good, because the application stays portable and testable without a cluster
* Good, because no public-facing workload holds Kubernetes credentials
* Good, because there is one component responsible for turning a resource into a running instance
* Bad, because configuration changes require a pod restart
* Bad, because the application cannot surface its own state to Kubernetes, so the operator has to infer it
