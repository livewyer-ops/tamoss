# Task Commands

This is the supported command surface. Operator-facing workflows are listed
first. Maintainer and helper commands are separated so internal plumbing is not
confused with the product install path.

This page is manually maintained. Refresh it against `task -l` when task
names or descriptions change.

## Operator-Facing Workflows

### Primary Local Commands

| Command | Purpose |
| --- | --- |
| `task kind:up PROFILE=local-kind` | Local Kind evaluation path: build local images, create or reuse Kind, apply the operator, apply the selected `Tamoss` instance, and ingest one playable demo segment. `PROFILE=multi-server` uses a multi-node Kind cluster. |
| `task env:summary ENV_DIR=deploy/environments/local-kind KUBECONFIG=tams.kubeconfig` | Print lifecycle status, access URLs, app credentials, API token, OAuth client details, and storage credentials for a Kind or remote environment. |
| `task kind:down` | Delete the disposable Kind cluster and local runtime state. |
| `task kind:e2e PROFILE=local-kind` | Recreate Kind, deploy the selected profile with the previous operator, run deployed checks, and finish with an in-place upgrade to the current operator. `PROFILE=multi-server` validates on a multi-node Kind cluster. |
| `task logs` | Show recent concise task logs from `.local/logs/task`. |

### Remote Kubernetes Commands

| Command | Purpose |
| --- | --- |
| `task env:init NAME=my-prod PROFILE=single-server DOMAIN=tamoss.example.com` | Create a remote environment composition from checked-in templates. |
| `task env:apply ENV=my-prod KUBECONFIG=/path/to/kubeconfig` | Apply the Helmfile platform releases, TAMOSS operator, and selected environment overlay. |
| `task env:diff ENV=my-prod KUBECONFIG=/path/to/kubeconfig` | Diff the Helmfile platform releases, TAMOSS operator, and selected environment overlay. |
| `task env:wait ENV=my-prod KUBECONFIG=/path/to/kubeconfig` | Wait for the selected environment's `Tamoss` resource to report `Ready=True`. |
| `task env:status ENV=my-prod KUBECONFIG=/path/to/kubeconfig` | Show the selected environment's `Tamoss` status, namespace resources, routes, and recent events. |
| `task env:summary ENV=my-prod KUBECONFIG=/path/to/kubeconfig` | Print lifecycle status, access URLs, app credentials, API token, OAuth client details, and storage credentials for the selected environment. |

### Additional Validation Commands

| Command | Purpose |
| --- | --- |
| `task e2e:deployed PROFILE=local-kind KUBECONFIG=tams.kubeconfig` | Run deployed checks against an existing target without creating a cluster; first run syncs Python test dependencies and installs Playwright Chromium. |
| `task kind:profiles:e2e` | Run `local-kind`, `single-server`, and `multi-server` e2e sequentially on one Kind cluster name; each profile recreates the cluster with its configured Kind topology. |
| `task test:media:fixtures` | Validate managed-ingest fixture timing with containerized `ffprobe`. |

Validation commands print compact suite labels such as `test frontend.unit` and
deployed checks such as `e2e api.storage-object-lifecycle`. JUnit artifacts
use stable `reports/junit-*.xml` names.

## Maintainer Workflows

### Development Commands

| Command | Purpose |
| --- | --- |
| `task setup` | Install local Python/frontend dev dependencies. |
| `task dev` | Run native API and frontend dev servers with Compose dependencies. |
| `task deps` | Start local Compose dependencies only. |
| `task check` | Fast confidence gate: lint plus focused tests. |
| `task test` | Fast local test subset. |
| `task test:smoke` | Run deployed API/UI smoke and verify the operator Chainsaw smoke labels. |
| `task lint` | Lint Python and frontend code. |
| `task security:audit` | Run local security audits. |
| `task test` | Run the fast local Python and frontend subset. |

### Quality and Contract Commands

| Command | Purpose |
| --- | --- |
| `task lint:mypy` | Run Python mypy checks. |
| `task lint:ruff:lint` | Check Python import order and formatting. |
| `task lint:ruff:fix` | Fix Python import order and formatting. |
| `task lint:eslint` | Check frontend formatting, types, and lint. |
| `task lint:shell` | Check task shell helper syntax. |
| `task lint:yamlfmt` | Validate YAML formatting. |
| `task openapi:check` | Verify BBC OpenAPI preprocessing is reproducible and TAMOSS parity passes. |
| `task openapi:parity` | Check TAMOSS OpenAPI parity against the BBC spec. |
| `task openapi:sync` | Regenerate the vendored BBC OpenAPI derivative. |
| `task versions:check` | Check platform, compose, and operator-owned operand version pins. |
| `task security:audit:python` | Run Python dependency audit. |
| `task security:audit:frontend` | Run frontend dependency audit. |
| `task security:audit:osv` | Run OSV dependency and repository audit. |

### Operator Maintainer Commands

| Command | Purpose |
| --- | --- |
| `task operator:build` | Build the operator manager binary. |
| `task operator:test` | Run operator unit and envtest suites. |
| `task operator:manifests` | Generate CRDs, RBAC, and webhook manifests. |
| `task operator:template` | Render the operator Kustomize install. |
| `task operator:monitoring:template` | Render the optional Prometheus Operator monitoring overlay. |
| `task operator:monitoring:check` | Validate base and monitoring overlay renders. |
| `task operator:support-bundle KUBECONFIG=/path/to/kubeconfig TAMOSS_NAMESPACE=tams TAMOSS_NAME=tamoss-kind` | Collect a redacted Kubernetes diagnostic bundle. |
| `task operator:e2e:chainsaw:labels` | Verify every Chainsaw scenario has supported execution labels. |
| `task operator:e2e:chainsaw:render` | Run render-only Chainsaw tests without a Kubernetes cluster. |
| `task operator:e2e:chainsaw:smoke KUBECONFIG=/path/to/kubeconfig` | Run the Kind-backed Chainsaw smoke slice against an existing cluster. |
| `task operator:e2e:chainsaw:ci KUBECONFIG=/path/to/kubeconfig` | Run the pull-request Chainsaw slice against an existing cluster. |
| `task operator:e2e:chainsaw:ci:up` | Create a disposable Kind cluster and run the labelled Chainsaw CI slice. |
| `task operator:e2e:chainsaw:nightly KUBECONFIG=/path/to/kubeconfig` | Run extended Kind-backed Chainsaw coverage. |
| `task operator:e2e:chainsaw:release KUBECONFIG=/path/to/kubeconfig` | Run release-labelled Chainsaw checks. |
| `task operator:e2e:chainsaw:deployed KUBECONFIG=/path/to/kubeconfig` | Run read-only Chainsaw checks against an already-installed TAMOSS cluster. |
| `task operator:e2e:chainsaw:focus SELECTOR='test.tamoss.io/domain=storage' KUBECONFIG=/path/to/kubeconfig` | Run a labelled Chainsaw slice. |
| `task operator:e2e:chainsaw KUBECONFIG=/path/to/kubeconfig` | Run Chainsaw operator tests against an existing cluster. |
| `task operator:e2e:chainsaw:up` | Run Chainsaw tests on a disposable Kind cluster. |

### Helper Commands

| Command | Purpose |
| --- | --- |
| `task ingest VIDEO=/path/to/video.mp4` | Convenience arbitrary-media ingest helper for local evaluation and testing; may require local media tooling. |

Helper commands are not the public deployment path and may be hidden from
`task -l`.
