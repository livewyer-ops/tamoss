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

## Inner Loop

Fast iteration helpers for controller development. Make targets own the Go
toolchain (build, test, lint, codegen); task targets own clusters, e2e suites,
and CI orchestration. Reach for `make` when you are editing Go, and for `task`
when you need a cluster or the full gate.

Run the envtest suite without regenerating code or running fmt/vet:

```bash
make -C operator test-fast        # envtest suite only; run `make test` before pushing
go test ./internal/controller/... # single package, from operator/
```

Reload a controller change into an existing Kind cluster without rerunning
`task kind:up` (rebuilds the image, loads it, restarts the deployment):

```bash
task kind:operator:reload
```

Run the controller manager on the host against a cluster:

```bash
task operator:run KUBECONFIG=tams.kubeconfig
```

Caveats: `operator:run` scales the in-cluster operator to zero (scale it back
up when finished) and sets `ENABLE_WEBHOOKS=false`, so the delete-protection
webhook server is not started on the host. If the in-cluster webhook
configuration is installed, deletes of `Tamoss`/`StorageBackend` resources
will be rejected while the deployment is scaled down (`failurePolicy: Fail`).

Focus a single chainsaw test instead of the full suite:

```bash
task operator:e2e:chainsaw:focus KUBECONFIG=/path/to/kubeconfig -- <test-dir-name-or-regex>
task operator:e2e:chainsaw:focus KUBECONFIG=/path/to/kubeconfig SELECTOR='test.tamoss.io/suite=smoke'
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
