# TAMOSS Operator

This directory contains the TAMOSS Kubernetes operator. Public install and
operations guidance lives under `../docs/`; this README is for maintainers
working on operator code and generated artifacts.

## Public Docs

- [Architecture](../docs/concepts/architecture.md)
- [Install](../docs/operations/install.md)
- [Day-2 Operations](../docs/operations/day-2.md)
- [Tamoss CR Reference](../docs/reference/tamoss-cr.md)
- [StorageBackend CR Reference](../docs/reference/storagebackend-cr.md)
- [Testing](../docs/development/testing.md)

## Common Commands

Run commands from the repository root:

```bash
task operator:build
task operator:test
task operator:manifests
task operator:template
```

Run operator e2e tests:

```bash
task operator:e2e:chainsaw KUBECONFIG=/path/to/kubeconfig
task operator:e2e:chainsaw:up
```

## Generated Artifacts

- CRDs, RBAC, and webhook manifests are generated from Go API/controller code.
- The public operator Kustomize install is rendered under `deploy/operator`.

Regenerate after changing API types, RBAC markers, or webhook configuration:

```bash
task operator:manifests
task operator:install-manifest
```

## Install Boundary

The public TAMOSS install path is the checked-in deploy tree. Use:

```bash
kubectl apply -k deploy/platform/<profile>
kubectl apply --server-side -k deploy/operator
kubectl apply -k deploy/environments/<name>
```

Local source builds use:

```bash
task kind:up PROFILE=local-kind
```

## License

Licensed under the Apache License 2.0. See `../LICENSE`.
