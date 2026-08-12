# Operator chainsaw e2e tests

This directory contains the Chainsaw suite for TAMOSS operator behaviour. Each
test case owns its resources and assertions locally:

```text
operator/test/chainsaw/<capability>/<scenario-kebab-case>/
  chainsaw-test.yaml
  resources/
  assert/
```

Shared fixtures live under `fixtures/` only when more than one scenario uses
the same setup. Scenario behaviour belongs in the scenario directory, not in a
shared dispatcher. Repeated readiness checks belong under `fixtures/assert/`
before a scenario adds another local copy.

## Assertion Style

Use native Chainsaw operations for Kubernetes state:

- `apply` creates fixtures and CRs.
- `assert` checks resource existence, status conditions, owner references,
  Deployment specs, Secret keys, finalizers, and Events.
- `delete` removes Kubernetes resources when the delete is expected to
  succeed.
- `patch` mutates Kubernetes resources when the mutation is the behaviour under
  test.
- `wait` handles direct readiness and deletion waits.
- `apply` with an `expect` check on `$error` proves that the Kubernetes API
  rejected a submitted resource. The Chainsaw `error` operation only asserts
  an error state; it does not submit the file and must not be used as an
  admission test.

Event names often include API-server generated suffixes; that is not a reason
to use shell. Assert a partial `v1/Event` with the expected `reason` and
`involvedObject` fields when the scenario only needs to prove that the Event
exists.

Use Chainsaw operations in this order:

1. Use `apply`, `create`, `patch`, `delete`, `wait`, `assert`, and `error` for
   Kubernetes resources and controller convergence.
2. Use `proxy` for read-only HTTP reachability checks served by a Kubernetes
   Pod or Service.
3. Use `command` only for an external system boundary, such as PostgreSQL, S3,
   operator process configuration, a mutating API workflow, or a response-body
   assertion that cannot use `proxy` because Chainsaw 0.2.15 does not propagate
   proxy outputs.

Inline `script` operations, shell entrypoints, shell pipelines, and `kubectl`
polling are not allowed. A command at an external boundary must invoke one
purpose-built executable directly and have an explicit Chainsaw `check`.
Fixture containers follow the same rule: invoke `pg_isready`, `aws`, or the
fixture programme directly and use Kubernetes Job retries or probes for
convergence.

Run against the current Kubernetes context:

```bash
task operator:e2e:chainsaw KUBECONFIG=/path/to/kubeconfig
```

Run a standard labelled group:

```bash
task operator:e2e:chainsaw:smoke KUBECONFIG=/path/to/kubeconfig
task operator:e2e:chainsaw:ci KUBECONFIG=/path/to/kubeconfig
task operator:e2e:chainsaw:nightly KUBECONFIG=/path/to/kubeconfig
task operator:e2e:chainsaw:release KUBECONFIG=/path/to/kubeconfig
```

Create a disposable Kind cluster, build and load the operator image, apply the
checked-in Chainsaw operator overlay, run the suite, and tear the cluster down:

```bash
task operator:e2e:chainsaw:up
```

CI uses the labelled Kind wrapper:

```bash
task operator:e2e:chainsaw:ci:up
```

The disposable run renders `operator/config/chainsaw` with unrestricted local
loading so its only base is the checked-in `deploy/operator/install.yaml`
distribution. The overlay adds test-only operator settings and expects the
stable local image tag `livewyer/tamoss-operator:chainsaw`. The wrapper builds
or retags `OPERATOR_IMAGE` to that tag before loading it into Kind, so custom
images remain possible without generated Kustomize overlays:

```bash
OPERATOR_IMAGE=example.com/tamoss-operator:dev task operator:e2e:chainsaw:up
```

Run one case:

```bash
task operator:e2e:chainsaw:focus -- fresh-cluster-install-via-single-manifest
```

Run a labelled focus:

```bash
task operator:e2e:chainsaw:focus KUBECONFIG=/path/to/kubeconfig SELECTOR='test.tamoss.io/domain=storage'
```

The suite uses one chainsaw namespace per case, prefixed
`tamoss-chainsaw-`. Namespaces are deleted after successful tests. For failure
triage, re-run with `CHAINSAW_SKIP_DELETE=true` or use the CI artefacts under
`reports/chainsaw-logs/`.

On CI failure, `.github/scripts/collect-chainsaw-diagnostics.sh` writes global
operator logs under `reports/chainsaw-logs/global/` and one bundle per retained
Chainsaw namespace under `reports/chainsaw-logs/namespaces/<namespace>/`. Each
namespace is one test case instance, so the bundle contains the case-local
`Tamoss` YAML, `kubectl describe` output for workloads and storage backends,
and namespace Events without dumping Secret data.

## Execution Labels

Every `chainsaw-test.yaml` declares these labels so the same scenario folders
can serve local, CI, release, and operations workflows. `.tasks/lib/chainsaw_labels.py`
is the executable source of truth for label values and selector presets.

| Label | Values |
| --- | --- |
| `test.tamoss.io/target` | `kind`, `deployed`, `external` |
| `test.tamoss.io/tier` | `smoke`, `standard`, `extended`, `release` |
| `test.tamoss.io/domain` | `install`, `instance`, `storage`, `db`, `auth`, `routing`, `schema`, `profile`, `observability`, `operations` |
| `test.tamoss.io/lifecycle` | `read-only`, `ephemeral`, `destructive` |
| `test.tamoss.io/provider` | `none`, `cnpg`, `rustfs`, `authentik`, `external`, `mixed` |
| `test.tamoss.io/profile` | `none`, `local-kind`, `edge`, `single-server`, `multi-server` |

| Command | Selection |
| --- | --- |
| `task operator:e2e:chainsaw:smoke` | Kind-backed smoke tests |
| `task operator:e2e:chainsaw:ci` | Kind-backed smoke and standard tests, excluding external-provider tests |
| `task operator:e2e:chainsaw:nightly` | Kind-backed smoke, standard, and extended tests |
| `task operator:e2e:chainsaw:release` | Release-labelled checks |
| `task operator:e2e:chainsaw:deployed` | `target=deployed,lifecycle=read-only` only |
| `task operator:e2e:chainsaw:focus SELECTOR='...'` | Any explicit Chainsaw label selector |

Use directory focus when editing one scenario. Use label focus when validating a
domain, provider, lifecycle class, or CI/release slice.

Deployed-cluster Chainsaw tests are read-only. They must validate existing
status, routes, or API health without creating, deleting, or mutating `Tamoss`
or `StorageBackend` resources. Mutation, delete-protection, cascade deletion,
and idempotency checks stay in Kind-backed groups.

Shared fixtures currently include:

- `operator-namespace-rbac.yaml` grants the already-installed operator access
  to a scenario namespace.
- `external-backend-secrets.yaml` creates the conventional Postgres and S3
  Secret names used by external-backend scenarios.
- `postgres.yaml` starts a single-pod PostgreSQL 18 instance with ephemeral
  storage.
- `rustfs.yaml` starts a single-pod RustFS S3-compatible backend and creates the
  `tamoss` bucket using `amazon/aws-cli`.
- `rustfs-crd.yaml` installs a minimal RustFS Operator `Tenant` CRD for tests
  that assert TAMOSS emits RustFS Operator resources without running the RustFS
  controller.
- `gateway-api-crds.yaml` installs only Gateway API resource types needed for
  reconciler output assertions.

Shared assertion fixtures under `fixtures/assert/` are mandatory for common
readiness checks that more than one scenario needs:

- `postgresql-available.yaml` checks the fixture PostgreSQL Deployment.
- `rustfs-available.yaml` checks the fixture RustFS Deployment.
- `rustfs-bucket-ready.yaml` checks the fixture RustFS bucket creation Job.
- `operator-ready.yaml` checks the installed operator Deployment and webhook
  Endpoints.

Optional shared assertion fixtures cover provider or state that only some
scenarios need:

- `authentik-available.yaml` checks the Authentik fixture Deployment.
- `tamoss-all-components-ready.yaml` checks a standard `tamoss` CR that has
  reached `Ready` with `AllComponentsReady`.

Keep readiness assertions local when they also prove scenario-specific state,
such as resolved resource names, deterministic backend IDs, delete-protection
state, first-start names, or multiple instances.

The `lib/` directory is intentionally empty. Do not add shared shell dispatchers
there for Kubernetes state that native Chainsaw assertions can express.

Before adding a new case, add or locate the matching scenario heading,
kebab-case the heading, then add a matching test directory with an explicit
`chainsaw-test.yaml` file. Keep the directory shape
even for single-file cases; it leaves room for local `resources/` and `assert/`
files without flattening scenario ownership into `<scenario>.yaml` files.

Chainsaw is the boundary test for behaviour that requires a real Kubernetes
API server and controller loop: watches, convergence, Jobs, provider contracts,
finalisers, recovery, isolation, and destructive lifecycle operations. Put
pure validation, rendering, defaulting, and status permutations in Go unit or
envtest coverage. Extend an existing lifecycle journey when a new assertion
shares its setup and transition; do not add a scenario for each field or each
previously observed defect.

## Operational Groups

- `operator-install-distribution/` covers installing the operator distribution
  and integrating it with external services. Static base and monitoring-overlay
  renders are validated by `task operator:monitoring:check`.
- `tamoss-instance-reconciliation/` covers single-instance CR rendering,
  validation, immutable fields, resource ownership, and update behaviour.
- `tamoss-operational-behaviour/` covers cross-cutting day-two contracts such
  as idempotency, multiple instances, recovery actions, drift, and events.
- `storagebackend-reconciliation/` covers `StorageBackend` lifecycle,
  registration, delete protection, diagnostics, deletion cleanup, and API
  storage selection behaviour.
- `cnpg-backend/`, `rustfs-operator-backend/`, and
  `authentik-blueprints-identity/` cover provider-specific behaviour that only
  makes sense for that provider.
- `auth-runtime-modes/` covers identity mode rendering that is not specific to
  Authentik, including disabled auth and external OAuth/OIDC.
- `database-schema-management/` covers schema migration state, gating, fixture
  loading, and terminal failure behaviour.
- `ingest-lifecycle/` covers the release-critical `FlowProfile` to `IngestRun`
  journey through the pinned TAMSin image, namespace-local input storage, TAMS
  registration, and controller cleanup.
- `profile-rendering/` covers profile-rendered Kubernetes shape without
  duplicating full Kind bootstrapping.

Profile bootstrapping is coordinated with the Kind profile e2e tasks. Chainsaw
may assert profile-rendered Kubernetes shape, but full cluster creation and
through-ingress smoke checks stay in the existing Kind workflow.

## Rollout Gates

Some rollout checks are intentionally outside source control. Branch protection
must be updated by repository administrators after the `operator-chainsaw-e2e`
job has stayed green on `main`. The ten-run flake soak and any deferred HA
leader-election scenario should be recorded on the pull request or follow-up
issue that enables those gates, not hidden inside the suite.
