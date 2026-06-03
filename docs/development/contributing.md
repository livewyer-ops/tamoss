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
task kind:test PROFILE=local-kind
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

Run the normal operator gate:

```bash
task operator:test
```

Use the detailed Chainsaw tasks only when changing operator reconciliation or
lifecycle behaviour. Those commands are documented in
`operator/test/chainsaw/README.md`.

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
