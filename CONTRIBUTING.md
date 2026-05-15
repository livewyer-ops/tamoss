# Contributing to TAMOSS

Local setup, test commands, and PR workflow for TAMOSS.

## Prerequisites

The fastest path is [aqua](https://aquaproj.github.io/docs/install/) —
one install step gives you the pinned CLI set for local development
(Task, uv, kind, kubectl, helm, helmfile, OSV Scanner, node,
yamlfmt, jq, gh).
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
- `curl`, `openssl`, `git`

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
task up      # create Kind cluster + deploy helmfile
task down    # tear it down
```

Mental model: **dev = speed, Kind = confidence, remote = release
validation.** Develop against `task dev`; gate merges against `task up`
+ `task e2e`; use the remote cluster only for final rollout checks.

## Testing

Test code lives under `tests/`, organised by what it tests:

- `tests/adapters/bbc/` — maintained in-process BBC contract and semantic tests.
- `tests/e2e/` — deployed black-box BBC workflow checks against Kind or a
  remote target file.

Run the compatibility gates that block merges:

```bash
task test:contract:bbc  # BBC v8.0 OpenAPI/path/status parity
task test:semantics:bbc # BBC resource lifecycle and workflow semantics
task test:bbc           # both BBC gates above
task test:all           # BBC gates, workers, and frontend
```

Other focused suites:

```bash
task test:adapters          # configured adapter integration checks
task test:adapters:postgres # Postgres repository persistence checks
task test:adapters:storage  # configured object-storage checks
task test:workers           # asynchronous deletion and webhook workers
task test:frontend # frontend unit/component suite
task test:coverage # backend coverage report over the BBC gates
```

End-to-end (Kind + deploy + deployed ingress tests):

```bash
task e2e
task test:deployed DEPLOY_ENV=kind KUBECONFIG=tams.kubeconfig
```

The local `task e2e` path is intentionally broader than the CI Kind workflow:
it runs the BBC in-process gates and then the deployed ingress suite. CI splits
those checks into backend, frontend, and Kind workflows.

Test markers in use:

- `bbc` — BBC TAMS v8.0 contract or semantic obligation.
- `e2e` — deployed BBC-facing black-box checks.
- `needs_db` — requires a live PostgreSQL endpoint.
- `needs_s3` — requires a real S3/RustFS endpoint.
- `regression` — fixture-sensitive tests, excluded from the BBC suite.
- `slow` — takes >10s to complete.
- `smoke` — fast high-confidence subset.
- `worker` — asynchronous deletion and webhook processing checks.

Prefer adding new BBC-facing behavior coverage under the contract or semantic
task that matches the failure mode. Add deployed checks only when the behavior
requires a real ingress, browser, object store, or worker deployment.

## Code organisation

- **Runtime**: `src/app/tamoss/`, split into API routes, application use
  cases, domain records, adapters, auth, settings, and worker entry points.
- **OpenAPI contract**: `src/openapi.yaml`, rebuilt from the vendored BBC spec.
  Runtime implementation lives under `src/app/tamoss/`.
- **Tests**: maintained TAMOSS BBC parity tests live under
  `tests/adapters/bbc/` and deployed black-box flows under `tests/e2e/`.
- **Database**: `db/schema.sql`, `db/bootstrap.sql`, and `db/fixtures.sql`.
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
`task test:bbc`, then commit the submodule gitlink together with any
resulting `src/openapi.yaml` change.

## Pull requests

Before opening a PR:

```bash
git fetch upstream && git rebase upstream/main
uv run --project src pre-commit run --all-files
task check
task security:audit
task test:bbc
task e2e                                 # if your change touches
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

## Getting help

- Bugs and feature requests: [GitHub Issues](https://github.com/livewyer-ops/tamoss/issues)
- Questions: [GitHub Discussions](https://github.com/livewyer-ops/tamoss/discussions)
- Security disclosure: [SECURITY.md](SECURITY.md)

## License

Contributions are licensed under the [Apache License 2.0](LICENSE).
