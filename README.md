<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="src/app/frontend/public/tamoss-logo-transparent.png">
    <source media="(prefers-color-scheme: light)" srcset="docs/assets/tamoss-logo-light-transparent.png">
    <img src="docs/assets/tamoss-logo-light-transparent.png" alt="TAMOSS logo" width="260">
  </picture>
</p>

# TAMOSS

[![License: Apache 2.0](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)
[![Python 3.14](https://img.shields.io/badge/python-3.14-blue)](https://www.python.org/)
[![BBC TAMS v8.2](https://img.shields.io/badge/BBC%20TAMS-v8.2-green)](https://github.com/bbc/tams)

TAMOSS is a Kubernetes-native implementation of the
[BBC TAMS v8.2 API specification](https://github.com/bbc/tams). It installs as
an operator-driven product with four supported infrastructure profiles:
`local-kind`, `edge`, `single-server`, and `multi-server`.

The operator reconciles `Tamoss`, `StorageBackend`, `FlowProfile`, and
`IngestRun` custom resources into the API, worker, UI, schema migration,
generated Secrets, routing, Profile registration, and selected backend
integrations.

## TAMS compatibility

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="src/app/frontend/public/brand/tams-lockup-color-dark.svg">
  <source media="(prefers-color-scheme: light)" srcset="src/app/frontend/public/brand/tams-lockup-color-light.svg">
  <img src="src/app/frontend/public/brand/tams-lockup-color-light.svg" alt="TAMS" width="220">
</picture>

TAMOSS implements version 8.2 of the
[BBC TAMS API specification](https://github.com/bbc/tams). Compatibility is
verified by the project's maintained
[TAMS conformance gates](docs/development/testing.md#unit-and-contract-gates),
including contract, semantic, integration, and deployed tests. The TAMS mark
identifies the specification TAMOSS implements; it is not a claim of TAMS or
BBC certification or endorsement.

## Features

- **TAMS-compatible media store**: Implements the BBC TAMS v8.2 API for working
  with sources, flows, flow segments, tags, storage backends, webhooks, and
  deletion workflows.
- **Operator-managed runtime**: Reconciles API, worker, UI, schema migration,
  generated Secrets, routing, and backend integration from Kubernetes custom
  resources.
- **Deployment profiles**: Ships `local-kind`, `edge`, `single-server`, and
  `multi-server` profiles so the same operator path works from local evaluation
  through production-shaped clusters.
- **Interchangeable platform services**: Supports managed or external
  PostgreSQL, S3-compatible storage, OAuth2/OIDC authentication, and HTTP
  ingress without changing the client-side install flow.
- **Operational web UI**: Provides a browser interface for browsing TAMOSS
  records, checking runtime state, and exercising selected API-backed actions.
- **Day-2 controls**: Reports readiness through status conditions and Events,
  corrects managed-resource drift, and protects destructive resource deletion.

## Quickstart

**Prerequisites:** Docker, `curl`, `git`, and
[aqua](https://aquaproj.github.io/docs/install) — a single-binary CLI version
manager. With aqua installed, the rest of the toolchain (`task`, `kind`,
`kubectl`, `helm`, `helmfile`, `chainsaw`, …) is provisioned by `aqua install`.

Use the local [Kind](https://kind.sigs.k8s.io/) profile first:

```bash
aqua install
export PATH="$(aqua root-dir)/bin:$PATH"

task kind:up PROFILE=local-kind
```

The summary prints the app URL, app username/password, API docs URL, API token,
OAuth client details, and storage credentials. Then inspect the instance:

```bash
kubectl --kubeconfig tams.kubeconfig -n tams get tamoss,pods,svc,ingress
```

Open:

- API docs: <https://api.tamoss.localtest.me/docs>
- Web UI: <https://app.tamoss.localtest.me>
- S3 endpoint: <https://s3.tamoss.localtest.me>
- Authentik: <https://auth.tamoss.localtest.me>

To install on an existing Kubernetes cluster instead of the disposable
Kind cluster, use the environment workflow:

```bash
task env:init NAME=my-prod PROFILE=multi-server DOMAIN=tamoss.example.com
$EDITOR deploy/environments/my-prod/platform-values.yaml
$EDITOR deploy/environments/my-prod/tamoss-patch.yaml
task env:apply ENV=my-prod KUBECONFIG=/path/to/kubeconfig
task env:wait ENV=my-prod KUBECONFIG=/path/to/kubeconfig
```

Remote environments are composition roots: `platform-values.yaml` configures the
[Helmfile](https://helmfile.readthedocs.io/)-managed platform releases, and the
[Kustomize](https://kustomize.io/) overlay applies the
`Tamoss` resources.
Generated remote environments default to public ACME TLS through
`ClusterIssuer/tamoss-public`; set the ACME email in `platform-values.yaml`
before applying. Use `tls.mode: existing` for a pre-installed ClusterIssuer or
`tls.mode: disabled` when TLS Secrets are supplied outside
[cert-manager](https://cert-manager.io/).
The raw apply sequence is:

```bash
(
  cd deploy/platform
  helmfile --kubeconfig "$KUBECONFIG" \
    --file helmfile.yaml.gotmpl \
    --state-values-file values/defaults.yaml \
    --state-values-file ../../deploy/environments/<name>/platform-values.yaml \
    sync \
    --sync-args "--server-side=true --rollback-on-failure" \
    --wait \
    --wait-for-jobs
)
kubectl apply --server-side -k deploy/operator
kubectl apply -k deploy/environments/<name>
```

## Profiles

| Profile | Use when | Default backing services |
| --- | --- | --- |
| `local-kind` | You want to evaluate or develop TAMOSS on Kind. | Local reference platform with [CNPG](https://cloudnative-pg.io/), [RustFS](https://github.com/rustfs/rustfs) Operator, [Authentik](https://goauthentik.io/), cert-manager, and [Traefik](https://traefik.io/) on host port 443. |
| `edge` | You run one ARM64 node, such as a Raspberry Pi 4 with 4 GB memory. | Compact single-node ARM64 topology; bearer-token auth by default, with optional managed Authentik. |
| `single-server` | You run one Kubernetes node or a small self-managed cluster. | Single-node workload topology; platform services are selected by the environment. |
| `multi-server` | You run production-shaped self-managed Kubernetes. | Replicated workload topology; platform services are selected by the environment. |

Use `multi-server` as the production reference profile. External PostgreSQL,
S3-compatible storage, OAuth2/OIDC, and ingress providers can be used where the
`Tamoss` or `StorageBackend` resource selects an external provider.

## Documentation

| Guide | Description |
| --- | --- |
| [Documentation Map](docs/README.md) | Full documentation structure |
| [Local Kind](docs/getting-started/local-kind.md) | Start locally on Kind |
| [Install](docs/operations/install.md) | Apply platform, operator, and environment layers |
| [Profiles](docs/concepts/profiles.md) | Understand supported profile defaults |
| [Provider Ownership](docs/concepts/provider-ownership.md) | Managed vs external responsibilities |
| [Storage Backends](docs/concepts/storage-backends.md) | Default and additional object-store backends |
| [Troubleshooting](docs/operations/troubleshooting.md) | Diagnose install and runtime failures |
| [Task Commands](docs/reference/task-commands.md) | Current operational command surface |
| [Contributing](CONTRIBUTING.md) | Development and contribution workflow |

## Development

For product development:

```bash
task setup
task dev
task check
```

For deployed confidence:

```bash
task kind:test PROFILE=local-kind
```

See [Development Workflow](docs/development/contributing.md) and
[Testing](docs/development/testing.md).

## Community

- Bug reports and feature requests: [GitHub Issues](https://github.com/livewyer-ops/tamoss/issues)
- Chat: [#tamoss in the Cloud Native Agile Production Slack](https://join.slack.com/t/cnap-media/shared_invite/zt-46so866cy-YstgGBzIgcA6U1c3J1Rg1g)
- Discussions: [GitHub Discussions](https://github.com/livewyer-ops/tamoss/discussions)
- Security vulnerabilities: see [SECURITY.md](SECURITY.md)

## Related Projects

- [BBC TAMS specification](https://github.com/bbc/tams)

## License

Licensed under the [Apache License 2.0](LICENSE).

This project implements the BBC TAMS v8.2 specification. See
[src/vendor/bbc-tams/](src/vendor/bbc-tams/) for upstream license information.
