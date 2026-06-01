# Testing

Use the smallest test that proves the change, then widen the scope when the
change touches shared behavior or deployed contracts.

## Fast Local Gate

```bash
task check
task test:fast
```

`task check` runs lint plus the fast focused test subset. `task test:fast`
runs the pure local Python subset directly: BBC semantics, local storage
adapters, application architecture checks, workers, scripts, and support
helpers. It excludes database, S3/RustFS, media-tooling, deployed e2e, and
operator-cluster tests by marker.

## Unit and Contract Gates

```bash
task test
task test:bbc
task test:media:fixtures
task openapi:check
```

`task test:bbc` covers in-process BBC TAMS contract checks.
`task test:media:fixtures` validates the tiny managed-ingest media fixture with
containerized `ffprobe`, so developers do not need unmanaged local media tools.

Validation tasks print compact suite labels before runner output. Common labels
include `test py.bbc.contract`, `test py.bbc.semantics`,
`test frontend.unit`, `test operator.go`, and `test operator.chainsaw`.

JUnit reports are written under `reports/` with stable names such as
`junit-py-bbc-contract.xml`, `junit-frontend-unit.xml`, and
`junit-operator-chainsaw.xml`.

The report names identify the affected quality area:

| Area | Report |
| --- | --- |
| BBC API contract | `junit-py-bbc-contract.xml`, `junit-py-bbc-semantics.xml` |
| Python local fast gate | `junit-py-fast.xml` |
| Python application | `junit-py-application.xml`, `junit-py-workers.xml` |
| Frontend | `junit-frontend-unit.xml` |
| Media fixtures | `junit-media-fixtures.xml` |
| Deployed product | `junit-e2e-deployed-<profile>.xml` |
| Operator | `junit-operator-*.xml`, `junit-e2e-operator-*.xml` |

## Deployed Gates

Run against an existing local Kind target:

```bash
task test:smoke PROFILE=local-kind KUBECONFIG=tams.kubeconfig
task e2e:deployed PROFILE=local-kind KUBECONFIG=tams.kubeconfig
```

`task test:smoke` runs the deployed smoke slice for the common API/UI workflows,
including demo-media retrieval, storage lifecycle, webhook delivery, UI ingress,
UI ingest, and playback preview, then validates the operator Chainsaw label set
so the explicit operator smoke slice remains selectable. These tasks sync the
Python dev dependencies and install Playwright Chromium on first run, then reuse
them on later runs. Use `task operator:e2e:chainsaw:smoke` when you specifically
want the ephemeral operator smoke scenarios as well.

The deployed e2e suite prints check IDs such as `e2e api.demo-media`,
`e2e api.storage-object-lifecycle`, `e2e auth.oidc-discovery`, and
`e2e ui.ingest-upload`. Its JUnit report is
`reports/junit-e2e-deployed-<profile>.xml`.

Recreate Kind, deploy the selected profile, and run the full gate:

```bash
task kind:e2e PROFILE=local-kind
```

The full Kind gate is destructive for the selected Kind cluster. It bootstraps
the profile with the previous operator image, runs the deployed API/UI checks,
then upgrades the same cluster to the current operator image and verifies the
`Tamoss` workloads stay in place.

Validate all public profiles sequentially on Kind:

```bash
task kind:profiles:e2e
```

## Operator Gates

```bash
task operator:test
task operator:e2e:chainsaw:render
task operator:e2e:chainsaw:smoke KUBECONFIG=/path/to/kubeconfig
task operator:e2e:chainsaw:ci KUBECONFIG=/path/to/kubeconfig
task operator:e2e:chainsaw KUBECONFIG=/path/to/kubeconfig
task operator:e2e:chainsaw:ci:up
```

Chainsaw tests are selected by `test.tamoss.io/*` labels. Use `render` for
Kustomize/render-only checks, `smoke` for a small local Kind gate, `ci` for the
pull-request operator slice, `nightly` for extended Kind coverage, and
`release` for release-labelled checks. `task operator:e2e:chainsaw:deployed`
is reserved for read-only checks against an already-installed cluster.

Some operator tests use envtest. Use `task operator:test` rather than direct
`go test ./...` for the full suite; the task delegates to `operator/Makefile`,
which downloads the pinned `setup-envtest` helper and sets `KUBEBUILDER_ASSETS`
for the downloaded Kubernetes control-plane binaries. Direct `go test` runs can
reuse repo-local envtest binaries after they have been downloaded, but a clean
checkout still needs `task operator:test` to bootstrap them first.

The envtest suite installs TAMOSS CRDs plus the provider CRDs it needs for typed
or unstructured test resources: CNPG CRDs from
`operator/test/fixtures/cnpg-crds.yaml` and the RustFS Tenant CRD from
`deploy/platform/chart/files/rustfs-operator/tenant-crd.yaml`. Provider
controllers are not started by envtest; tests that need provider status still
simulate it explicitly.
