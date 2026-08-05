# Testing

Use the smallest test that proves the change, then widen the scope when the
change touches shared behaviour or deployed contracts.

## Fast Local Gate

```bash
task check
task test:fast
```

`task check` runs lint plus the fast focused test subset. `task test:fast`
runs the pure local Python subset directly: local TAMS in-process checks,
local storage adapters, application architecture checks, workers, scripts, and
support helpers. It excludes database,
S3/[RustFS](https://github.com/rustfs/rustfs), media-tooling, deployed e2e,
and operator-cluster tests by marker.

## Unit and Contract Gates

```bash
task test
task test:tams
task test:tams:deployed
task test:media:fixtures
task openapi:check
```

`task test:tams` is the local TAMS conformance gate: OpenAPI parity, semantic
checks, and focused real Postgres/S3 checks. `task test:tams:conformance` is an
alias for the same local gate. Run `task deps` first when local Postgres and
RustFS are not already running. `task test:tams:deployed` runs the deployed
TAMS slice against a target env file.
`task test:media:fixtures` validates the small managed-ingest media fixture with
containerised `ffprobe`, so developers do not need unmanaged local media tools.

Validation tasks print compact suite labels before runner output. Common labels
include `test py.tams.contract`, `test py.tams.semantics`,
`test frontend.unit`, `test operator.go`, and `test operator.chainsaw`.

JUnit reports are written under `reports/` with stable names such as
`junit-py-tams-contract.xml`, `junit-frontend-unit.xml`, and
`junit-operator-chainsaw.xml`.

The report names identify the affected quality area:

| Area | Report |
| --- | --- |
| TAMS API contract | `junit-py-tams-contract.xml`, `junit-py-tams-semantics.xml`, `junit-py-tams-integration.xml`, `junit-py-tams-deployed-<profile>.xml` |
| Python local fast gate | `junit-py-fast.xml` |
| Python application | `junit-py-application.xml`, `junit-py-workers.xml` |
| Frontend | `junit-frontend-unit.xml` |
| Media fixtures | `junit-media-fixtures.xml` |
| Deployed product | `junit-e2e-deployed-<profile>.xml` |
| Operator | `junit-operator-*.xml`, `junit-e2e-operator-*.xml` |

## Deployed Gates

Run against an existing local [Kind](https://kind.sigs.k8s.io/) target:

```bash
task test:smoke PROFILE=local-kind KUBECONFIG=tams.kubeconfig
task test:tams:deployed PROFILE=local-kind KUBECONFIG=tams.kubeconfig
task e2e:deployed PROFILE=local-kind KUBECONFIG=tams.kubeconfig
```

`task test:smoke` runs the deployed smoke slice for the common API/UI workflows,
including demo-media retrieval, storage lifecycle, webhook delivery, UI ingress,
UI ingest, and playback preview, then validates the operator
[Chainsaw](https://kyverno.github.io/chainsaw/) label set
so the explicit operator smoke slice remains selectable. These tasks sync the
Python dev dependencies and install Playwright Chromium on first run, then reuse
them on later runs. Use the Chainsaw maintainer workflow when you specifically
want the ephemeral operator smoke scenarios as well.

The deployed e2e suite prints check IDs such as `tams deployed.demo-media`,
`tams deployed.storage-object-lifecycle`, `e2e auth.oidc-discovery`, and
`e2e ui.ingest-upload`. Its JUnit report is
`reports/junit-e2e-deployed-<profile>.xml`.

Create or reuse Kind, deploy the selected profile, and run the deployed gate:

```bash
task kind:test PROFILE=local-kind
task kind:e2e PROFILE=local-kind
```

`task kind:test` is the normal Kind confidence command. It creates or reuses
the selected cluster, deploys the current images, and runs deployed TAMS plus
product API/UI checks. `task kind:e2e` is the destructive fresh-cluster variant.

## Operator Gates

```bash
task operator:test
```

Use `task operator:test` for the normal operator gate. Chainsaw split tasks are
still available for operator-maintainer and CI work; they are documented in
`operator/test/chainsaw/README.md`.

Some operator tests use envtest. Use `task operator:test` rather than direct
`go test ./...` for the full suite; the task delegates to `operator/Makefile`,
which downloads the pinned `setup-envtest` helper and sets `KUBEBUILDER_ASSETS`
for the downloaded Kubernetes control-plane binaries. Direct `go test` runs can
reuse repository-local envtest binaries after they have been downloaded, but a
clean checkout still needs `task operator:test` to bootstrap them first.

The envtest suite installs TAMOSS CRDs plus the provider CRDs it needs for typed
or unstructured test resources: CNPG CRDs from
`operator/test/fixtures/cnpg-crds.yaml` and the RustFS Tenant CRD from
`deploy/platform/charts/rustfs-operator/templates/tenant-crd.yaml`. Provider
controllers are not started by envtest; tests that need provider status still
simulate it explicitly.
