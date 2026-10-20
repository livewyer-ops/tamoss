# CRD versioning

TAMOSS serves and stores `tamoss.livewyer.io/v1alpha1` for the namespaced
`Tamoss`, `StorageBackend`, `FlowProfile`, `IngestRun` and `TamossHibernate`
resources. There is no conversion webhook or additional served version.

The API is alpha. Breaking field, default or validation changes require
migration guidance in the [changelog](../../CHANGELOG.md). Check that guidance
before applying new CRDs to an existing cluster. Kubernetes CEL transition
rules protect immutable resource identity fields.

`apiVersion` selects the Kubernetes resource schema. `Tamoss.spec.version`
selects the product release for an instance. Changing the operator installation
or CRD does not change an instance's selected product release.

Status records observations. Use documented conditions and
`status.observedGeneration` when checking whether the operator has processed
the current spec. Secret values are not exposed in status.

See the [Tamoss](tamoss-cr.md), [StorageBackend](storagebackend-cr.md),
[FlowProfile](flowprofile-cr.md) and [IngestRun](ingestrun-cr.md) references for
field details, and [Upgrades](../operations/upgrades.md) for the upgrade workflow.
