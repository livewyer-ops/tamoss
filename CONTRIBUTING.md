# Contributing to TAMOSS

Local setup, test commands, and PR workflow for TAMOSS.

## Prerequisites

The fastest path is [aqua](https://aquaproj.github.io/docs/install/) —
one install step gives you the pinned CLI set for local development
(Task, uv, kind, kubectl, Go,
kubeconform, OSV Scanner, node, yamlfmt, jq, gh).
See [aqua.yml](aqua.yml) for the full list.

```bash
git clone https://github.com/livewyer-ops/tamoss.git
cd tamoss
aqua install
export PATH="$(aqua root-dir)/bin:$PATH"
```

Add the `PATH` line to your shell startup file if you use aqua for this
repo's tools.

Required at OS level for the local development and Kubernetes paths:

- Docker or Podman (+ `docker compose`)
- `curl`, `git`

Also install `ffmpeg` and `uuidgen` if you use the ingest helper.

## Fork and branch

```bash
# Fork on GitHub, then
git clone https://github.com/<your-username>/tamoss.git
cd tamoss
git remote add upstream https://github.com/livewyer-ops/tamoss.git
git checkout -b feat/your-feature-name   # or fix/your-bug-fix
```

## Development loop

Bring up the API + frontend in dev mode:

```bash
task dev
```

Full local Kubernetes stack (used for integration sign-off):

```bash
task kind:up PROFILE=local-kind
task kind:down
```

Mental model: **dev = speed, Kind = confidence, remote = release
validation.** Develop against `task dev`; gate merges against
`task kind:test PROFILE=local-kind`; use the remote cluster only for final
rollout checks.

### Operator development

The Kubernetes operator lives under `operator/` and uses Go with
controller-runtime tooling. The operator has an independent Go
module and release cadence from the Python API and Node UI.

Use the root Taskfile wrappers where possible:

```bash
task operator:build
task operator:test
task operator:manifests
```

Use `task operator:test` for the normal operator gate. The detailed
[Chainsaw](https://kyverno.github.io/chainsaw/)
commands are CI/operator-maintainer tools; use them when changing reconciliation
semantics or lifecycle behaviour. The test layout and contribution workflow are
documented in
[`operator/test/chainsaw/README.md`](operator/test/chainsaw/README.md).

Work in `src/app/` normally does not require Go. Install the aqua toolchain
before touching the operator so `go`, `kubeconform`, and
`chainsaw` match CI.

## Testing

Test code lives under `tests/`, organised by what it tests:

- `tests/tams/conformance/` — maintained in-process TAMS contract and semantic tests.
- `tests/tams/integration/` — focused TAMS checks that need real Postgres and
  S3/[RustFS](https://github.com/rustfs/rustfs).
- `tests/tams/deployed/` — deployed TAMS conformance checks against Kind or a
  remote target file.
- `tests/e2e/` — deployed product API/UI checks against Kind or a remote target
  file.

Run the compatibility gates that block merges:

```bash
task test      # fast local Python and frontend subset
task test:tams # local TAMS conformance, including real Postgres/RustFS checks
```

Run `task deps` before `task test:tams` when local Postgres and RustFS are not
already running. Use `task test:tams:contract`, `task test:tams:semantics`, or
`task test:tams:integration` only when you need a narrower CI-style slice.

Other focused suites:

```bash
task test:adapters          # configured adapter integration checks
task test:adapters:postgres # Postgres repository persistence checks
task test:adapters:storage  # configured object-storage checks
task test:workers           # asynchronous deletion and webhook workers
task test:frontend # frontend unit/component suite
task test:tams:coverage # backend coverage report over the TAMS gates
```

End-to-end (Kind + deploy + deployed ingress tests):

```bash
task kind:test PROFILE=local-kind
task e2e:deployed PROFILE=local-kind KUBECONFIG=tams.kubeconfig
```

`task kind:test` creates or reuses the local Kind cluster, deploys the selected
profile, and runs deployed checks. `task e2e:deployed` runs the same deployed
checks against an existing target file.

Test markers in use:

- `tams_conformance` — TAMS API, ADR, or AppNote conformance requirement.
- `tamoss_extension` — TAMOSS compatibility or operational extension outside core TAMS conformance.
- `tamoss_security` — TAMOSS authentication, authorisation, or hardening policy.
- `e2e` — deployed TAMS-facing black-box checks.
- `needs_db` — requires a live PostgreSQL endpoint.
- `needs_s3` — requires a real S3/RustFS endpoint.
- `slow` — takes >10s to complete.
- `smoke` — fast high-confidence subset.
- `worker` — asynchronous deletion and webhook processing checks.

Prefer adding new TAMS-facing behaviour coverage under the contract or semantic
task that matches the failure mode. Add deployed checks only when the behaviour
requires a real ingress, browser, object store, or worker deployment.

## Code organisation

- **Runtime**: `src/app/tamoss/`, split into API routes, application use
  cases, domain records, adapters, auth, settings, and worker entry points.
- **OpenAPI contract**: `src/openapi.yaml`, rebuilt from the vendored BBC spec.
  Runtime implementation lives under `src/app/tamoss/`.
- **Tests**: maintained TAMS conformance tests live under
  `tests/tams/conformance/`, focused DB/S3 conformance tests under
  `tests/tams/integration/`, deployed TAMS checks under
  `tests/tams/deployed/`, and deployed product API/UI checks under
  `tests/e2e/`.
- **Database**: canonical SQL assets live under
  `src/app/tamoss/db/migrations/assets/`; Alembic applies those files at
  runtime.
- **Vendored BBC reference**: `src/vendor/bbc-tams/` is shipped
  upstream-as-is. Do not repair its Markdown links, examples, or ADR text in
  this repo; exclude it from local doc/link assertions and update the submodule
  only as a deliberate contract update.

Normal setup uses the commit pinned by this repository:

```bash
git submodule update --init --recursive
```

Do not use `git submodule update --remote` during routine setup. To
move to a newer BBC tag or commit, check out the desired commit inside
`src/vendor/bbc-tams/`, run `task openapi:check` and
`task test:tams`, then commit the submodule gitlink together with any
resulting `src/openapi.yaml` change.

## Pull requests

Before opening a PR:

```bash
git fetch upstream && git rebase upstream/main
uv run --project src pre-commit run --all-files
task check
task security:audit
task test:tams
task kind:test PROFILE=local-kind        # if your change touches
                                         # deployment, storage, or IO
```

### PR description

Include:

- **Title** — clear, under 70 characters.
- **Summary** — what changes and why.
- **Related issues** — `Fixes #123`, `Closes #42`.
- **Test plan** — how you validated the change.
- **Breaking changes** — call them out explicitly.

### PR checklist

- [ ] Code follows style guide (pre-commit passes).
- [ ] Tests added or updated.
- [ ] `task check` and relevant focused suites are green.
- [ ] Docs updated (README, usage, deployment, or configuration as relevant).

## Commit messages

Write a clear, imperative subject line that states the change. Keep it short
enough to scan in history, and use the body for rationale, migration notes, or
validation details when the subject cannot carry the context. Include issue
references such as `Closes #42` when they apply.

```text
Correct S3 presigned URL expiration

Presigned URLs were expiring too quickly; extended to 1 hour.

Fixes #89
```

## Reporting bugs and suggesting features

- **Bugs**: check [issues](https://github.com/livewyer-ops/tamoss/issues)
  for duplicates, then open one with reproduction steps, expected vs.
  actual behaviour, and environment details.
- **Features**: open an issue with the `enhancement` label; describe
  the use case and why it would benefit TAMOSS users.
- **Security**: do not open a public issue — see [SECURITY.md](SECURITY.md).

For operational bugs, include the selected profile, `Tamoss` status conditions,
provider ownership choices, relevant Events, and redacted log excerpts. Do not
include Secret values, tokens, passwords, private keys, or complete presigned
URLs in public issues.

## Getting help

- Bugs and feature requests: [GitHub Issues](https://github.com/livewyer-ops/tamoss/issues)
- Chat: the `#tamoss` Slack channel, invite in [SUPPORT.md](SUPPORT.md)
- Questions: [GitHub Discussions](https://github.com/livewyer-ops/tamoss/discussions)
- Security disclosure: [SECURITY.md](SECURITY.md)

## License

Contributions are licensed under the [Apache License 2.0](LICENSE).
