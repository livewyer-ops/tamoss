<p align="center">
  <img src="src/app/frontend/public/tamoss-icon.png" alt="TAMOSS logo" width="128">
</p>

# TAMOSS (Time Addressable Media Open Source Store)

[![License: Apache 2.0](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)
[![Python 3.11](https://img.shields.io/badge/python-3.11-blue)](https://www.python.org/)
[![BBC TAMS v8.0](https://img.shields.io/badge/BBC%20TAMS-v8.0-green)](https://github.com/bbc/tams)

TAMOSS implements the [BBC TAMS API specification](https://github.com/bbc/tams)
and ships Kubernetes deployment assets plus an optional web UI addon for
browsing and operating segment-level media workflows.

Naming is deliberate: TAMOSS is used for the product, runtime, packages, and
container images; TAMS is used only for the BBC protocol, specification, and
resource model.

## Features

- **TAMS-compatible media store**: Implements the BBC TAMS v8.0 API for working
  with sources, flows, flow segments, tags, storage backends, webhooks, and
  deletion workflows.
- **Time-addressed segment catalogue**: Records segment metadata, timing,
  format, object references, and flow relationships so clients can discover and
  assemble media over time.
- **Object-store media access**: Uses S3-compatible storage and presigned URLs
  for media upload and retrieval without proxying media bytes through the API.
- **Operational web UI**: Provides a browser interface for exploring TAMS
  resources and creating or deleting records.
- **Preview media workflows**: Includes preview-status UI and helper paths for
  ingesting evaluation media and checking browser playback from registered
  segment URLs.
- **Automation hooks**: Supports webhook notifications and background deletion
  workers for integration with downstream systems.
- **Deployable by default**: Ships Helm profiles and Kind automation for
  running a complete local or remote Kubernetes deployment.

## Quickstart

If you are new to TAMOSS, start with the local Kubernetes deployment path.

### Recommended: Run TAMOSS on Kind

Use this path when you want to deploy and use the product locally.

- Runtime: Kubernetes with Kind
- Guide: [Deployment Guide](docs/deployment.md)

Prerequisites for this path:

- [Docker](https://docs.docker.com/get-docker/) (must be installed separately)
- [aqua](https://aquaproj.github.io/docs/install/) — installs the rest of
  the CLI toolchain at the versions CI uses (Task, kind, kubectl, helm,
  helmfile, uv, node, yamlfmt, jq). See [aqua.yml](aqua.yml)
  for the full pin list.

If you prefer to install the CLIs yourself instead of using aqua, grab
[Task](https://taskfile.dev/docs/installation),
[kind](https://kind.sigs.k8s.io/docs/user/quick-start/#installation),
[kubectl](https://kubernetes.io/docs/tasks/tools/),
[helm](https://helm.sh/docs/intro/install/), and
[helmfile](https://helmfile.readthedocs.io/en/latest/) at matching versions.

```bash
# One-time: install pinned CLI tools declared in aqua.yml
aqua install
export PATH="$(aqua root-dir)/bin:$PATH"

task up
kubectl --kubeconfig tams.kubeconfig get pods -n tams
```

Add the `PATH` line to your shell startup file if you use aqua for this repo's
tools.

> **Local TLS note**
> The Kind install uses self-signed certificates for the local `*.tamoss.localtest.me`
> hostnames. Before logging in, open <https://s3.tamoss.localtest.me> once and
> accept the browser warning. The Web UI and Auth Portal will prompt when visited,
> but the S3 endpoint is first used in the background during ingest/playback;
> accepting it up front prevents browser policy blocks during upload.

Once Kind is running, open:

- **Web UI**: <https://app.tamoss.localtest.me>
- **API docs**: <https://api.tamoss.localtest.me/docs>
- **Auth Portal**: <https://auth.tamoss.localtest.me>

If those hostnames do not load on your machine, see the hosts-file note in
[docs/deployment.md](docs/deployment.md#access-the-deployed-services).

## Choose Your Path

### Deployment Path

Use this path when you want to deploy and use TAMOSS.

- Runtime: Kubernetes
- Targets:
  - local Kubernetes with Kind
  - remote Kubernetes clusters
- Guide: [Deployment Guide](docs/deployment.md)

### Development Path

Use this path when you are changing the product itself.

- Runtime: native API + frontend dev servers, backed by a thin Compose
  dependency stack for PostgreSQL and RustFS/S3
- Purpose: local development, hot reload, product work
- Guide: [CONTRIBUTING.md](CONTRIBUTING.md)

Quick start:

Stop the Kind stack before starting the native dev servers; both paths expose
PostgreSQL and RustFS on the same local ports.

```bash
task setup           # install Python and frontend deps; create .env if needed
task dev             # run API + frontend dev servers (auto-starts containerised deps)
task check           # fast local gate: lint + unit + frontend tests
task security:audit  # local OSV, pip-audit, and npm audit checks
```

## Prerequisites

The fastest way to get every CLI this repo expects is
[aqua](https://aquaproj.github.io/docs/install/) — one install step gives
you Task, uv, kind, kubectl, helm, helmfile, node, yamlfmt, jq,
and gh at pinned versions for local development. CI workflows and container
builds pin their own runtimes where those environments need different packaging
or base images. See [aqua.yml](aqua.yml) for the full list.

```bash
git clone https://github.com/livewyer-ops/tamoss.git
cd tamoss
aqua install
export PATH="$(aqua root-dir)/bin:$PATH"
```

Required at the OS level for the local Kubernetes quickstart:

- [Docker](https://docs.docker.com/get-docker/) or Podman (+ `docker compose`)
- `curl`, `openssl`, `git`

Also install `ffmpeg` and `uuidgen` if you use the ingest helper.

If you would rather install each CLI by hand, see the individual upstream
sites: [Task](https://taskfile.dev/docs/installation),
[uv](https://docs.astral.sh/uv/getting-started/installation),
[kind](https://kind.sigs.k8s.io/docs/user/quick-start/#installation),
[kubectl](https://kubernetes.io/docs/tasks/tools/),
[helm](https://helm.sh/docs/intro/install/),
[helmfile](https://helmfile.readthedocs.io/en/latest/),
[npm](https://docs.npmjs.com/cli/v11/configuring-npm/install).

## API Surface

The TAMOSS API is faithful to the upstream
[BBC TAMS v8.0 specification](https://github.com/bbc/tams). Operational
endpoints such as `/healthz` and `/readyz` are product health endpoints, not
TAMS resources.

## Documentation

| Guide                                            | Description                                      |
| ------------------------------------------------ | ------------------------------------------------ |
| [Deployment](docs/deployment.md)                 | Deploy and use TAMOSS on Kind or remote Kubernetes |
| [Production](docs/production.md)                 | Operate TAMOSS on a generic remote Kubernetes cluster |
| [Usage](docs/usage.md)                           | Use the web UI, API, and ingest helper           |
| [Troubleshooting](docs/troubleshooting.md)       | Common issues and fixes                          |
| [Configuration](docs/configuration.md)           | Runtime and Helm configuration reference         |
| [Contributing](CONTRIBUTING.md)                  | Local dev setup, test workflow, PR process       |
| [Security](SECURITY.md)                          | Responsible disclosure and deployment guidance   |
| [Support](SUPPORT.md)                            | Public support and vulnerability-reporting paths |
| [Changelog](CHANGELOG.md)                        | Release notes                                   |

## Contributing

We welcome contributions of all kinds; bug reports, feature requests, documentation
improvements, and code. Please read [CONTRIBUTING.md](CONTRIBUTING.md) to get started.

All contributors are expected to follow our [Code of Conduct](CODE_OF_CONDUCT.md).

## Community

- **Bug reports and feature requests**: [GitHub Issues](https://github.com/livewyer-ops/tamoss/issues)
- **Discussions**: [GitHub Discussions](https://github.com/livewyer-ops/tamoss/discussions)
- **Security vulnerabilities**: See [SECURITY.md](SECURITY.md) for responsible disclosure

## Related Projects

- [BBC TAMS specification](https://github.com/bbc/tams): The upstream API spec this
  project implements

## License

Licensed under the [Apache License 2.0](LICENSE).

This project implements the BBC TAMS v8.0 specification. See
[src/vendor/bbc-tams/](src/vendor/bbc-tams/) for upstream license information.
