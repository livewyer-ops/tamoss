# Edge

`edge` is the single-node ARM64 profile for self-contained TAMOSS installs.
The target shape is one ARM64 node with local persistent storage; a
Raspberry Pi 4 class machine is the minimum reference. API access is bearer-token by default; the profile can
also run the managed [Authentik](https://goauthentik.io/) OAuth stack on the
same node within a 4 GB memory budget.

## Requirements

- One 64-bit ARM Linux node with at least 4 GB of memory: a Raspberry Pi 4
  Model B, a comparable single-board computer, or a small ARM cloud
  instance.
- Any 64-bit ARM Linux distribution. [K3s](https://k3s.io/) is the reference
  Kubernetes distribution.
- Persistent storage backed by an SSD or equivalent. Avoid SD-card-backed
  database and object storage.
- On single-board computers: active cooling, a stable power supply, and
  wired Ethernet.
- A default StorageClass, or explicit [CNPG](https://cloudnative-pg.io/) and
  [RustFS](https://github.com/rustfs/rustfs) storage classes in the
  `Tamoss` CR.
- [cert-manager](https://cert-manager.io/), [Traefik](https://traefik.io/),
  CNPG, and RustFS Operator installed by the TAMOSS platform layer unless the
  cluster already owns those services.
- Local DNS or host entries for API, UI, S3, and (with OAuth) auth hostnames.

Install K3s without its bundled Traefik when the TAMOSS platform installs the
pinned Traefik release for this profile.

## Install

Every `task` command below runs from a clone of this repository with the
pinned toolchain on the PATH. Install [aqua](https://aquaproj.github.io/docs/install)
and run `aqua install` first, as shown in the
[repository quickstart](../../README.md#quickstart).

### Provision K3s on the node

On a blank node running any 64-bit ARM Linux, such as Raspberry Pi OS Lite
64-bit, install K3s with its bundled Traefik disabled so the TAMOSS platform
can install the pinned Traefik release. Set `<node-address>` to the IP
address or DNS name you will manage the node through:

```bash
curl -sfL https://get.k3s.io | sh -s - --disable traefik --tls-san <node-address>
```

`--tls-san` adds `<node-address>` to the Kubernetes API certificate so
`kubectl` works from another machine. Omit it when you only run `kubectl` on
the node itself.

Copy the cluster credentials to your workstation and point them at the node:

```bash
mkdir -p ~/.kube
ssh <user>@<node-address> sudo cat /etc/rancher/k3s/k3s.yaml \
  | sed 's/127.0.0.1/<node-address>/' > ~/.kube/tamoss-edge.yaml
export KUBECONFIG=~/.kube/tamoss-edge.yaml
kubectl get nodes
```

The node reports `Ready` within about a minute. Run the remaining commands
from the workstation that holds this kubeconfig and a clone of this
repository.

### Validate the profile on Kind (optional)

This step is optional. To rehearse the profile locally on
[Kind](https://kind.sigs.k8s.io/) before touching the node:

```bash
task kind:up PROFILE=edge
```

### Install TAMOSS

For the K3s node provisioned above, `KUBECONFIG` is already set. For any
other existing ARM64 cluster, export the path to its kubeconfig first:

```bash
export KUBECONFIG=/path/to/kubeconfig
```

Create the environment composition, edit the two generated files, then
apply and inspect it:

```bash
task env:init NAME=my-edge PROFILE=edge DOMAIN=tamoss.edge
$EDITOR deploy/environments/my-edge/platform-values.yaml
$EDITOR deploy/environments/my-edge/tamoss-patch.yaml
task env:apply ENV=my-edge KUBECONFIG="$KUBECONFIG"
task env:wait ENV=my-edge KUBECONFIG="$KUBECONFIG"
task env:summary ENV=my-edge KUBECONFIG="$KUBECONFIG"
```

The generated edge platform values disable Authentik and use self-signed TLS
by default. Work through the [Key Settings](#key-settings) while
editing the two generated files, before `task env:apply`.

## Key Settings

### Auth: bearer token (default)

The operator defaults `edge` to token-only runtime auth. The API token lives
in the generated `<fullname>-api-token` Secret — `tams-api-token` for the
default fullname — under the `TAMOSS_API_TOKEN` key; `task env:summary` prints
the resolved value once the instance is ready. Read it into a variable and
send it as a bearer header:

```bash
export TAMOSS_API_TOKEN=$(kubectl -n tams get secret tams-api-token \
  -o jsonpath='{.data.TAMOSS_API_TOKEN}' | base64 -d)
curl -k -H "Authorization: Bearer $TAMOSS_API_TOKEN" https://api.tamoss.edge/
```

`-k` accepts the profile's default self-signed certificate; for real use,
trust the issuing CA on the client (for example with `--cacert`) instead of
disabling verification.

### Auth: managed OAuth

Declaring the Authentik provider runs the full OAuth stack on the node.
OAuth on edge needs the operator release that ships the spec-driven edge
Authentik defaults: the next release after 8.1.0-oss4.

On a live instance, apply the change in this order.

First, in `platform-values.yaml`, enable Authentik and set its ingress host
to `auth.` followed by your instance's `spec.publicEndpoint.baseDomain`
value. The operator derives the OAuth issuer hostname from the base domain,
so the two must agree. The generated platform values already contain the
`authentikChart` sizing block that bounds the Authentik server, worker, and
PostgreSQL for a 4 GB node; `task env:init PROFILE=edge` copies it from
[`deploy/platform/values/edge-reference.yaml`](../../deploy/platform/values/edge-reference.yaml).

```yaml
authentik:
  enabled: true
  ingress:
    host: auth.<base-domain>
```

Re-run `task env:apply` to roll out the Authentik stack.

Then switch the provider on the running instance with a merge patch. The
API server rejects a spec that carries both auth blocks, and `kubectl apply`
does not delete the old `external` block it does not own, so on a live
token-mode instance `task env:apply` alone fails that one-of admission
rule; the patch must come first. The generated composition keeps the
instance name `tamoss-edge` and the `tams` namespace from the checked-in
edge instance manifest:

```bash
kubectl -n tams patch tamoss tamoss-edge --type=merge \
  -p '{"spec":{"auth":{"providedBy":"authentik-blueprints","external":null}}}'
```

Finally, record the same change in `tamoss-patch.yaml` so later applies
agree with the cluster:

```yaml
  auth:
    providedBy: authentik-blueprints
    external: null
```

The operator submits the blueprint, waits for the issuer, and redeploys the
API and UI against it. The UI then redirects to the Authentik login. The
static API token stays valid in OAuth mode: the API accepts the generated
token as a bearer credential alongside Authentik-issued OAuth2 tokens.

### Memory budget

Steady-state memory use on a 4 GB ARM64 node:

| Mode | Node memory used |
| --- | --- |
| Bearer token | ~1.9 GiB |
| Managed OAuth | ~3.2 GiB |

Bearer mode fits a 4 GB node as installed. For OAuth mode, configure
SSD-backed swap before enabling Authentik; on a 4 GB node it is a
requirement, not a safety margin. The generated platform values bound the
Authentik components to fit this budget; do not lower the Authentik
PostgreSQL memory limit, which also carries the Authentik task queue.

### UI on and off

The UI serves static assets from nginx and holds ~3 MiB resident, so leaving
it enabled costs almost nothing. To disable it, add one line to the spec in
`tamoss-patch.yaml`:

```yaml
  ui:
    enabled: false
```

Removing the line (or setting `true`) restores the UI on the next reconcile.

### Storage

```yaml
  backends:
    db:
      cnpg:
        storage:
          size: 20Gi
    s3:
      rustfsOperator:
        pools:
          - name: pool-0
            servers: 1
            volumesPerServer: 4
            storage:
              size: 20Gi
```

Use larger PVC sizes on 128 GB or larger SSDs, and make backup ownership
explicit before accepting durable data.

## Validate

```bash
task e2e:deployed PROFILE=edge KUBECONFIG="$KUBECONFIG"
```

The checked-in target file behind this command,
[`tests/targets/edge.env`](../../tests/targets/edge.env), carries the Kind
validation hostnames. For a remote node whose hostnames differ, copy
[`tests/targets/remote.env.example`](../../tests/targets/remote.env.example)
into the environment directory, set the API, UI, and auth URLs plus
`TEST_TAMOSS_TOKEN_SECRET=tams-api-token` and
`TEST_TAMOSS_CR_NAME=tamoss-edge`, and point the checks at it:

```bash
task e2e:deployed PROFILE=edge KUBECONFIG="$KUBECONFIG" \
  TARGET_ENV=deploy/environments/my-edge/target.env
```

To validate the OAuth mode, apply the two [Key Settings](#key-settings)
changes to the environment and run the same deployed checks. The UI check
expects a redirect to the Authentik login instead of a direct 200.

The deployed checks exercise the API with the bearer token, certificate
state, and UI availability against the running instance.

## Operate

The profile also sets the following operator defaults:

- One replica each of API, worker, and UI.
- One CNPG PostgreSQL instance with a 10 GiB volume.
- One RustFS server with four 10 GiB volumes and disk checks bypassed for
  single-node local storage.
- Reduced API and worker runtime pools.
- Longer worker probe timeouts for ARM startup latency.
- TLS from `ClusterIssuer/tamoss-edge-selfsigned`.

See also:

- [Profiles](../concepts/profiles.md)
- [Install](../operations/install.md)
- [Provider Ownership](../concepts/provider-ownership.md)
