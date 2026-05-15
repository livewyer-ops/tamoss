# Deployment Guide

Deploying and using TAMOSS on Kubernetes.

If you want to change the product locally, use the development path instead:
- [CONTRIBUTING.md](../CONTRIBUTING.md)

## Quickstart

If you are new to TAMOSS, start with the Kind path:

```bash
aqua install
export PATH="$(aqua root-dir)/bin:$PATH"
task up
kubectl --kubeconfig tams.kubeconfig get pods -n tams
```

Then open:

- Web UI: <https://app.tamoss.localtest.me>
- API docs: <https://api.tamoss.localtest.me/docs>
- Auth Portal: <https://auth.tamoss.localtest.me>

The deployment path has two targets:

- `kind`: local Kubernetes for trying and using the product
- `remote`: an existing remote Kubernetes cluster

## Deployment Profiles

TAMOSS is kept as one Helm umbrella chart, `deploy/charts/tams-stack`, with two explicit
values profiles:

- `values-lite.yaml`: smallest operable TAMOSS deployment. It enables the API,
  PostgreSQL, standalone single-replica RustFS/S3-compatible storage, and
  generated API credentials. It disables UI, background workers, Gateway routing,
  authentik/forward-auth resources, and ingress by default.
- `values-full.yaml`: cloud-native deployment profile. It enables API,
  background workers, UI addon, PostgreSQL, RustFS/S3-compatible storage,
  Gateway API routes, ReferenceGrant, and forward-auth integration points. The
  chart profile assumes Gateway API CRDs, a compatible Gateway controller, and a
  forward-auth provider exist.

The local Full install path is `task up` because Helmfile also installs
cert-manager, Traefik, metrics-server, and authentik around the umbrella chart.

Render the umbrella chart profiles directly:

```bash
helm dependency build ./deploy/charts/tams-stack

helm template tams ./deploy/charts/tams-stack \
  --namespace tams \
  -f ./deploy/charts/tams-stack/values-lite.yaml \
  --set-file platform.dbInit.schemaSql=db/schema.sql \
  --set-file platform.dbInit.bootstrapSql=db/bootstrap.sql

helm template tams ./deploy/charts/tams-stack \
  --namespace tams \
  -f ./deploy/charts/tams-stack/values-full.yaml \
  --set-file platform.dbInit.schemaSql=db/schema.sql \
  --set-file platform.dbInit.bootstrapSql=db/bootstrap.sql
```

The supplied profile files default to release name and namespace `tams`. If a
target values file uses another release namespace, use that namespace
consistently for release-scoped resources and kubectl examples. If the release
name changes, override `tams.backends.*` to match the generated service names.

The database init assets use stock PostgreSQL tables, JSONB columns, and B-tree
and GIN indexes. No PostgreSQL extension is required by the TAMOSS schema.
Profile defaults load schema and bootstrap rows only. The Kind target enables
demo fixtures explicitly; remote and direct profile renders leave
`platform.dbInit.loadFixtures` false unless an operator opts in.

## Kind Path

This is the recommended starting point if you want to deploy and use TAMOSS locally on Kubernetes.

### Prerequisites

- [Docker](https://docs.docker.com/get-docker/)
- [aqua](https://aquaproj.github.io/docs/install/)

Install the repo-pinned CLI tools and put aqua's shim directory on `PATH`:

```bash
aqua install
export PATH="$(aqua root-dir)/bin:$PATH"
```

Add the `PATH` line to your shell startup file if you use aqua for this repo's
tools. If you install CLIs yourself instead of using aqua, install Task, kind,
kubectl, helm, and helmfile at versions compatible with `aqua.yml`.

## Environment Setup

Copy `.env.example` if you want to override local defaults:

```bash
cp .env.example .env
```

For the full variable reference see [configuration.md](./configuration.md).

### Deploy to Kind

The Kind path uses `deploy/kind.yaml` and Helmfile to run the full stack locally.

> **Note**: The direct deployment path is Helmfile-first.
> All local Kubernetes hostnames use `*.tamoss.localtest.me` (resolves to `127.0.0.1`) with self-signed TLS.

```bash
task up
```

This will:

1. Create a Kind cluster with 1 control-plane node (defined in `deploy/kind.yaml`)
2. Deploy services directly with Helmfile in dependency order:
   - **cert-manager** (TLS certificate management)
   - **Traefik** (ingress controller)
   - **metrics-server** (metrics API)
   - **TAMOSS stack** (platform resources, PostgreSQL, RustFS, TAMOSS API + UI)
   - **authentik** (authentication and OAuth2/OIDC)

Kubernetes deployments do not seed media or test data by default. The deployed
E2E suite creates BBC resources, uploads media through the allocated storage
URL, and cleans up the flow during the test.

```bash
# Recreate Kind, deploy the stack, and run supported checks.
task e2e

# Or verify an already deployed Kind stack.
task test:deployed DEPLOY_ENV=kind KUBECONFIG=tams.kubeconfig
```

### Files involved in the Kind path

- `deploy/kind.yaml`: Cluster topology, port mappings, node configuration
- `deploy/helmfile/`: Direct deployment entrypoint
- `deploy/targets/`: Target-specific Helmfile values
- `deploy/charts/tams-stack/`: Helm umbrella chart for single-namespace TAMOSS services and the app
- `deploy/charts/tams-platform/`: Repo-owned Kubernetes support resources for TAMOSS
- `deploy/charts/tams/`: TAMOSS application Helm chart

### Secrets

PostgreSQL, RustFS, and authentik bootstrap/OAuth secrets use this precedence:

1. `existingSecret`
   If set, the charts use that secret and do not create a replacement.
2. Explicit values
   If credentials are supplied in the target values, the charts create Kubernetes
   secrets from those values.
3. Generated fallback
   If neither of the above is provided, the charts generate credentials and preserve
   them across upgrades by reusing the existing in-cluster secret.

This lets local and test targets keep stable fixture credentials while remote
environments use generated in-cluster secrets or externally managed secrets.

### Machine Client OAuth2 Handoff

Full deployments can issue OAuth2 client-credentials tokens for machine clients.
For machine-client integrations, provide:

- Store ID: read from `GET /service/storage-backends` after deployment.
- API endpoint: the direct API hostname, for example `https://api.tamoss.localtest.me`.
- Segment URL Label: the browser-reachable storage label from the same storage-backends response.
- OAuth2 Token Endpoint: `https://auth.tamoss.localtest.me/application/o/token/` for Kind, or the equivalent remote auth hostname.
- Client ID: `tams-api-client` by default.
- Client Secret: Kubernetes secret `tams-authentik`, key `TAMOSS_OAUTH_CLIENT_SECRET`.
- Scope: optional by default. Request `tams-api/admin tams-api/read tams-api/write tams-api/delete`
  when you want the issued token to carry the BBC TAMS coarse-grained scopes.
- Token endpoint authentication method: send `client_id` and `client_secret` as
  token request form fields.
- TAMOSS validates OAuth2/OIDC bearer tokens by issuer, signature, optional
  audience, and optional required scopes. The shipped Kind, Full, and remote
  profiles do not require scopes by default so no-scope client-credentials
  tokens can call the API. Set `tams.auth.oauth2.requiredScopes` in site values
  when an installation must reject tokens that do not carry specific scopes.

The direct API hostname is not protected by authentik forward-auth in Full; it
relies on the API validating `Authorization: Bearer <token>`. The UI
hostname, including its `/api` proxy path, remains authentik-protected for
browser users.

Example token request shape:

```sh
CLIENT_SECRET="$(kubectl --kubeconfig tams.kubeconfig -n authentik \
  get secret tams-authentik \
  -o jsonpath='{.data.TAMOSS_OAUTH_CLIENT_SECRET}' | base64 --decode)"

curl -kfsS \
  -d grant_type=client_credentials \
  -d client_id=tams-api-client \
  -d client_secret="$CLIENT_SECRET" \
  https://auth.tamoss.localtest.me/application/o/token/
```

Add `-d "scope=tams-api/admin tams-api/read tams-api/write tams-api/delete"`
when the client or site policy needs the token to carry those scope claims.

The Kind endpoint uses a self-signed certificate. Drop `-k` when your endpoint
uses a certificate trusted by the client host.

The API validates the public issuer claim, but deployed profiles fetch JWKS
through the in-cluster authentik service so API pods do not depend on the
external auth hostname resolving from inside the cluster:

- Issuer: `https://auth.<domain>/application/o/tams-api/`
- JWKS: `http://authentik-server.authentik.svc.cluster.local/application/o/tams-api/jwks/`

### Inspect runtime logs

The API, worker, and UI write application logs to stdout/stderr. Use Kubernetes
logs and the component labels when you need a specific process:

```bash
kubectl --kubeconfig tams.kubeconfig logs -n tams \
  -l app.kubernetes.io/component=api --tail=100 -f

kubectl --kubeconfig tams.kubeconfig logs -n tams \
  -l app.kubernetes.io/component=worker --tail=100 -f
```

For browser-based clients, the Full profile configures edge CORS on the API
route for bearer-token reads from the configured UI origin. The Traefik
middleware must allow `Authorization` and `Content-Type` request headers. Add
any extra trusted browser origins explicitly in
`platform.middlewares.cors.accessControlAllowOriginList`; do not use wildcard
CORS with bearer-token API routes outside local experiments.

### Access the deployed services

Add to `/etc/hosts` (only required if your DNS provider enables DNS rebinding protection):

```text
127.0.0.1  app.tamoss.localtest.me api.tamoss.localtest.me s3.tamoss.localtest.me auth.tamoss.localtest.me
```

Then access:

- Web UI: <https://app.tamoss.localtest.me>
- API: <https://api.tamoss.localtest.me/docs>
- RustFS: <https://s3.tamoss.localtest.me>
- Auth Portal: <https://auth.tamoss.localtest.me>

### Manage the Kind cluster

```bash
# View pods
kubectl --kubeconfig tams.kubeconfig get pods -n tams

# View logs
kubectl --kubeconfig tams.kubeconfig logs -n tams deployment/tams -f

# Restart deployment
kubectl --kubeconfig tams.kubeconfig rollout restart -n tams deployment/tams

# Delete cluster and dev dependency containers
task down
```

### Reapply the Kind deployment

After changing values or code:

```bash
task deploy:kind KUBECONFIG=tams.kubeconfig
```

This reapplies the Kind Helmfile target without recreating the cluster.

## Remote Path

Remote clusters can be targeted directly without assuming this repo owns the cluster
CD system.

For production operations on a generic remote cluster, including rollout,
rollback, credentials, scaling, and smoke checks, see
[production.md](./production.md).

### Additional prerequisites

- [kubectl](https://kubernetes.io/docs/tasks/tools/)
- [helm](https://helm.sh/docs/intro/install/)
- [helmfile](https://helmfile.readthedocs.io/en/latest/)

### Deploy

```bash
task deploy:remote KUBECONFIG=/path/to/remote.kubeconfig
```

This uses the `remote` Helmfile target and applies the shared charts directly to the
target cluster. The default remote profile installs cert-manager, Traefik, Authentik, and the
full TAMOSS stack: API, background workers, UI, PostgreSQL, RustFS, and HTTPRoutes.

Remote product configuration lives in `deploy/targets/remote/values.yaml` using
the remote Helmfile target values schema: cluster service settings live under
`clusterServices` and `tamsServices`, platform settings under `platform`, and
TAMOSS application chart values under `tams`. Hostnames default to
`*.tamoss.example.com`; replace them or point DNS and TLS at the target cluster
before exposing it.

Use an extra Helmfile state-values file for site-specific overrides. This file
uses the same remote target values schema as `deploy/targets/remote/values.yaml`.
Put application overrides under the top-level `tams:` key. See
`deploy/targets/remote/site-values.example.yaml` for a minimal overlay.

```bash
task deploy:remote \
  KUBECONFIG=/path/to/remote.kubeconfig \
  DEPLOY_VALUES_FILE=/path/to/site-values.yaml
```

Set image tags explicitly when deploying images outside the chart defaults:

```bash
task deploy:remote KUBECONFIG=/path/to/remote.kubeconfig API_IMAGE_TAG=sha-3e05300 UI_IMAGE_TAG=sha-3e05300
```

For credentials, `remote` supports three target values shapes:

- point at externally managed secrets with `credentials.*.existingSecret`
- set explicit credentials in the target values
- omit credentials and let the charts generate them

The remote target enables NetworkPolicies with explicit ingress rules for the
API and UI, but leaves component egress open by default. Treat the shipped
policy as ingress isolation, not full namespace egress isolation. Tighten egress
in site values only after defining the cluster DNS, PostgreSQL, S3, OIDC, and
webhook delivery destinations your installation must reach.

When the repo-owned Gateway is enabled, it accepts HTTPRoutes from the TAMOSS
app namespace only. If your cluster uses a shared Gateway for multiple
namespaces, override `platform.gateway.allowedRoutes` deliberately and pair that
with cluster admission controls for who may attach routes.

Remote and direct Full-profile installs create the `traefik` GatewayClass when
enabled, but do not mark it as the cluster default. Only set
`platform.gatewayClass.default=true` when you own the cluster-wide GatewayClass
policy. The Kind profile keeps the local class as default for the repo-owned
development cluster.

### Validate deployment artifacts

```bash
task deploy:validate
```

This checks the Helmfile deployment path for both `kind` and `remote`, and also
renders and validates the chart-level Lite and Full profile values.

### Render manifests for install or GitOps

If you need rendered output for review, handoff, or a GitOps system, use Helmfile
directly instead of maintaining a second install wrapper.

Render the full stack for a target:

```bash
task deploy:template DEPLOY_ENV=kind KUBECONFIG=tams.kubeconfig > /tmp/tams-kind-rendered.yaml
task deploy:template DEPLOY_ENV=remote KUBECONFIG=/path/to/remote.kubeconfig > /tmp/tams-remote-rendered.yaml
task deploy:template \
  DEPLOY_ENV=remote \
  KUBECONFIG=/path/to/remote.kubeconfig \
  DEPLOY_VALUES_FILE=/path/to/site-values.yaml \
  > /tmp/tams-remote-rendered.yaml
```

Render a subset by Helmfile group:

```bash
task deploy:template DEPLOY_ENV=remote HELMFILE_GROUP=tams-stack KUBECONFIG=/path/to/remote.kubeconfig > /tmp/tams-stack.yaml
```

These rendered manifests are the supported export path when you want to feed another
install or GitOps system.

Rendered-manifest workflows must provide stable secret material. If PostgreSQL,
RustFS, authentik, OAuth client, or API-token values are left empty, the charts
generate credentials during rendering and the output changes on each run. Use
externally managed secrets (`credentials.*.existingSecret`) or explicit values
for `credentials.*`, `platform.authentikConfig.secrets.*`,
`platform.authentikConfig.oauth.clientSecret`, and
`tams.secrets.apiToken.token` before committing rendered manifests.

## Development Path

Development workflow lives outside the deployment guide.

If you want to change the product locally, use:

- [CONTRIBUTING.md](../CONTRIBUTING.md)

## Task Commands Reference

### Deployment and cluster management

| Command        | Description                                        |
| -------------- | -------------------------------------------------- |
| `task up`            | Create Kind cluster and deploy all services              |
| `task down`          | Tear down local runtime state                            |
| `task e2e`           | Recreate Kind and run the end-to-end stack checks        |
| `task deploy:remote` | Deploy the remote target directly with Helmfile          |
| `task deploy:validate` | Validate Helmfile deployment renders |

### Media operations

| Command            | Description               |
| ------------------ | ------------------------- |
| `task misc:ingest` | Ingest a custom MP4 video |

Run `task` with no arguments to print the supported task list.

## Understanding the Setup

### Kind (`deploy/kind.yaml`)

- Single control-plane node with custom port mappings:
  - `80` -> Traefik HTTP ingress
  - `443` -> Traefik HTTPS ingress
  - `55432` -> PostgreSQL NodePort used by local tests and dev tools
  - `9000` -> RustFS S3 API
  - `9001` -> RustFS console
- Uses kubeadm for cluster initialization
- Enables ingress with `ingress-ready=true` label

### Helmfile + Helm (`deploy/helmfile/`, `deploy/charts/`)

- Helmfile is the primary deployment entry point for Kind and remote
- Third-party services are installed directly from their charts
- Repo-owned resources are packaged in local charts, including the `tams-stack` Helm umbrella chart
- Shared target values live in `deploy/targets/`

## Environment Variables

For the full reference see [configuration.md](./configuration.md).

For the Kubernetes deployment path, the main configuration lives in:

- `deploy/targets/kind/values.yaml`
- `deploy/targets/remote/values.yaml`

## Technical Stack

- **API Framework**: FastAPI (Python 3.11)
- **Database**: PostgreSQL
- **Object Storage**: RustFS (S3-compatible)
- **Media Preview**: MPEG-TS segments with generated HLS manifests
- **Frontend addon**: React/TypeScript with experimental HLS.js playback preview
- **Container Orchestration**: Docker Compose / Kubernetes (Kind + Helmfile)
- **Package Manager**: uv (Python), npm (frontend)

## Related

- [usage.md](./usage.md): Using the UI and API
- [troubleshooting.md](./troubleshooting.md): Common issues and fixes
- [production.md](./production.md): Remote production operations
- [CONTRIBUTING.md](../CONTRIBUTING.md): Developer setup
- [configuration.md](./configuration.md): Full environment variable reference
