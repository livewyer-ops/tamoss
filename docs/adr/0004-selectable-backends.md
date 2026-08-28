---
status: "accepted"
---
# Selectable Backends Rather Than Bundled Ones

## Context and Problem Statement

A TAMOSS instance needs a PostgreSQL database, an S3-compatible object store and an identity provider.
Production estates usually already have some of these, often as managed services; a first-time evaluator has none of them.

An operator that only ever installed its own dependencies could not be deployed next to a managed database.
An operator that only ever consumed external ones would have no day-0 story.

## Considered Options

* Option 1: Always bundle managed instances of every dependency
* Option 2: Always require external dependencies
* Option 3: A `providedBy` discriminator per backend, selecting managed or external

## Decision Outcome

Chosen Option 3: A `providedBy` discriminator per backend.

Database is `external` or `cnpg`; S3 is `external` or `rustfs-operator`; auth is `external`, `none` or `authentik-blueprints` (`operator/api/v1alpha1/tamoss_backend_types.go`, `operator/api/v1alpha1/tamoss_auth_types.go`).
CEL validation rejects a spec whose backend block does not match its `providedBy`, so an inconsistent resource fails at apply time rather than at reconcile.

An earlier iteration also exposed `providedBy: bundled`, where the operator rendered the dependency directly rather than delegating to its operator.
That mode has been withdrawn, and the validation now rejects it.

**Confidence:** High.
Both paths are in use, and the discriminator absorbed the withdrawal of the `bundled` mode without needing a redesign.

**Reevaluate if:** the combination matrix grows beyond what the test suite can cover, or any backend needs a third provisioning mode.

### Consequences

* The choice is declarative and visible in the resource, rather than implied by which chart someone installed.
* Every backend concern is written at least twice, once for the managed path and once for the external one, and both need test coverage.
* Adding a provider is a CRD change, so the supported set is deliberately small.
* Managed and external paths have genuinely different lifecycles. Hibernate and resume support only some combinations, and `docs/operations/hibernate-resume.md` states which.

## Pros and Cons of the Options

### Option 1: Always bundle

* Good, because day-0 is a single apply with nothing to prepare
* Good, because there is one path to write and test
* Bad, because it cannot be deployed alongside a managed database or an existing identity provider, which rules out most production estates
* Bad, because it makes TAMOSS responsible for the backup, upgrade, and availability of components an operations team may already run better

### Option 2: Always require external

* Good, because it keeps TAMOSS to one job and one path
* Good, because it matches how the largest deployments would run it anyway
* Bad, because there is no day-0 experience: evaluating TAMOSS would first require standing up three dependencies
* Bad, because it conflicts with treating the project as a product

### Option 3: A `providedBy` discriminator

* Good, because one code path serves both an existing estate and a first install
* Good, because the choice is explicit in the resource and validated at admission
* Good, because each backend is chosen independently, so a managed database can sit beside an external object store
* Bad, because every backend concern is implemented and tested twice
* Bad, because the combinations multiply, and some are unsupported for operations such as hibernate and resume
