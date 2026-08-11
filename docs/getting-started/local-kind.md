# Local Kind

Use this local [Kind](https://kind.sigs.k8s.io/) path to evaluate TAMOSS with
the same operator flow used by the other profiles.

## Requirements

- Docker or a compatible container runtime.
- [`aqua`](https://aquaproj.github.io/) for the pinned toolchain, or
  equivalent versions of Task, Kind, kubectl, Helm, Helmfile, uv, yq, and jq.
- `curl` and `git`.

`aqua install` installs the pinned command-line tools used by the tasks. It
does not preinstall project virtualenvs or browser binaries; validation tasks
bootstrap those as needed on first run.

## Install

```bash
aqua install
export PATH="$(aqua root-dir)/bin:$PATH"

task kind:up PROFILE=local-kind
```

`task kind:up PROFILE=local-kind` does the following:

1. Creates or reuses the Kind cluster from `deploy/kind.yaml`.
2. Builds and loads local API, UI, and operator images.
3. Applies the Helmfile-managed platform from
   `deploy/environments/local-kind/platform-values.yaml`.
4. Applies the TAMOSS operator from `deploy/operator/local`.
5. Applies the `local-kind` instance overlay.
6. Waits for `Tamoss/tamoss-kind` to report `Ready=True`.
7. Ingests one small playable demo segment through the deployed API and storage
   backend, unless `KIND_DEMO_INGEST=false` is set.

`task kind:up` writes the cluster kubeconfig to `tams.kubeconfig` in the
repository root. The commands in this guide reference it explicitly.

The summary prints first-start lifecycle status, the app URL, app
username/password, API docs URL, API token, OAuth client details, and storage
credentials.

For a clean API validation target with no seeded demo flow/source:

```bash
task kind:up PROFILE=local-kind KIND_DEMO_INGEST=false
```

To print the same access details and current Kubernetes status again:

```bash
task env:summary ENV_DIR=deploy/environments/local-kind KUBECONFIG=tams.kubeconfig
```

## Key Settings

Everything in this profile is bundled and disposable: self-signed TLS,
`tamoss.localtest.me` hostnames, PostgreSQL and
[RustFS](https://github.com/rustfs/rustfs), and the managed
[Authentik](https://goauthentik.io/) stack, all inside one Kind cluster that
`task kind:up` creates and
`task kind:down` deletes. Nothing in it is intended to survive or be tuned. If
an override matters enough to keep, it belongs in an environment directory
targeting one of the other profiles.

## Access

The local Kind profile uses [Traefik](https://traefik.io/) on host port 443.
It does not require
application NodePorts or port-forwarding.

- API docs: <https://api.tamoss.localtest.me/docs>
- Web UI: <https://app.tamoss.localtest.me>
- S3 endpoint: <https://s3.tamoss.localtest.me>
- Authentik: <https://auth.tamoss.localtest.me>

Local Kind uses self-signed TLS. Browser trust is per origin, so accept or
trust the local certificate warning for each origin you open. Browser ingest
uploads directly to the S3 origin, so make sure
`https://s3.tamoss.localtest.me` is accepted as well as the app origin.
Operator-created Tamsin Jobs disable certificate verification only for this
disposable profile; every remote-capable profile retains strict TLS validation.

Inspect the install:

```bash
kubectl --kubeconfig tams.kubeconfig -n tams get tamoss,pods,svc,ingress
kubectl --kubeconfig tams.kubeconfig -n tams describe tamoss tamoss-kind
```

## Validate

Run deployed checks against the local ingress path:

```bash
task e2e:deployed PROFILE=local-kind KUBECONFIG=tams.kubeconfig
```

The first deployed validation run syncs the Python dev dependencies and
installs the Playwright Chromium browser used by the UI checks.

Use the broader local gate when you want a clean rebuild and test run:

```bash
task kind:e2e PROFILE=local-kind
```

This deletes and recreates the local Kind cluster, deploys the current operator,
and runs the deployed TAMS/product checks.

## Cleanup

```bash
task kind:down
```

`task kind:down` deletes the disposable Kind cluster and local runtime state. It
does not delete `Tamoss` resources individually before removing the cluster.

See also:

- [Install](../operations/install.md)
- [Troubleshooting](../operations/troubleshooting.md)
- [Task Commands](../reference/task-commands.md)
