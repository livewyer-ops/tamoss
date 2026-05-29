# Development Workflow

Use this guide when changing TAMOSS itself. For public deployment, use the
getting-started and operations guides instead.

## Local Native Loop

```bash
task setup
task dev
task check
```

`task dev` runs native API and frontend dev servers with the local Compose
dependency stack. Stop the Kind stack first because both paths use local
PostgreSQL and S3 ports.

## Kubernetes Confidence Loop

```bash
task kind:up PROFILE=local-kind
task e2e:deployed PROFILE=local-kind KUBECONFIG=tams.kubeconfig
```

Use this when changes affect Kubernetes manifests, operator behavior, ingress,
authentication, S3, or deployed UI/API integration.

## Operator Work

```bash
task operator:build
task operator:test
task operator:manifests
task operator:template
```

Run Chainsaw tests for operator semantics:

```bash
task operator:e2e:chainsaw:render
task operator:e2e:chainsaw:smoke KUBECONFIG=/path/to/kubeconfig
task operator:e2e:chainsaw:ci:up
```

Use `task operator:e2e:chainsaw:focus -- <case-name-or-regex>` for one
scenario, or `task operator:e2e:chainsaw:focus SELECTOR='test.tamoss.io/domain=storage'`
for a labelled slice.

Keep changes small and provable. A commit means the current iteration is ready
to build on.

## Python Style

Keep module docstrings sparse and meaningful. Package entry points, public
adapters, and scripts may use them to explain a boundary or execution purpose;
ordinary implementation modules should not gain boilerplate docstrings solely
for consistency.

See also:

- [Testing](testing.md)
- [Task Commands](../reference/task-commands.md)
- [CONTRIBUTING.md](../../CONTRIBUTING.md)
