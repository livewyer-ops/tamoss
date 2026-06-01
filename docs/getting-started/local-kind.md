# Local Kind

Use this local Kind path to evaluate TAMOSS with the same operator flow used by
the other profiles.

## Prerequisites

- Docker or a compatible container runtime.
- `aqua` for the pinned toolchain, or equivalent versions of Task, Kind,
  kubectl, Go, uv, node, jq, and yamlfmt.
- `curl`, `openssl`, and `git`.

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

1. Create or reuse the Kind cluster from `deploy/kind.yaml`.
2. Build and load local API, UI, and operator images.
3. Apply the Helm-managed platform from
   `deploy/environments/local-kind/platform-values.yaml`.
4. Apply the TAMOSS operator from `deploy/operator/local`.
5. Apply the `local-kind` instance overlay.
6. Wait for `Tamoss/tamoss-kind` to report `Ready=True`.
7. Ingest one tiny playable demo segment through the deployed API and storage
   backend.

The summary prints first-start lifecycle status, the app URL, app
username/password, API docs URL, and API token.

To print the same access details and current Kubernetes status again:

```bash
task kind:summary PROFILE=local-kind KUBECONFIG=tams.kubeconfig
```

## Access

The local Kind profile uses Traefik on host port 443. It does not require
application NodePorts or port-forwarding.

- API docs: <https://api.tamoss.localtest.me/docs>
- Web UI: <https://app.tamoss.localtest.me>
- S3 endpoint: <https://s3.tamoss.localtest.me>
- Authentik: <https://auth.tamoss.localtest.me>

Local Kind uses self-signed TLS. Browser trust is per origin, so accept or
trust the local certificate warning for each origin you open. Browser ingest
uploads directly to the S3 origin, so make sure
`https://s3.tamoss.localtest.me` is accepted as well as the app origin.

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

This deletes and recreates the local Kind cluster, runs the deployed checks,
and finishes with an in-place operator upgrade check on the same cluster.

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
