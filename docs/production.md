# Production Operations

Operational runbook for deploying and operating TAMOSS on a remote Kubernetes
cluster. This document is intentionally generic: cluster-specific kubeconfigs,
domains, storage classes, and secret values belong in private operator files, not
in this repository.

## Scope

This runbook covers the Helmfile remote deployment path and the operational
checks needed after a release. It assumes an operator supplies:

- a kubeconfig for the target cluster
- a site-specific values overlay
- DNS and TLS for the public hostnames
- a storage class for PostgreSQL and RustFS PVCs
- image tags from CI
- a credential strategy for production secrets

Raw credentials must not be committed. Store them in the cluster, an external
secret manager, or a private values overlay.

## Remote Deployment Model

The remote deployment path uses:

- Helmfile entrypoint: `deploy/helmfile/helmfile.yaml.gotmpl`
- Base remote values: `deploy/targets/remote/values.yaml`
- Optional site overlay: `DEPLOY_VALUES_FILE=/path/to/site-values.yaml`
- Minimal overlay example: `deploy/targets/remote/site-values.example.yaml`

The remote target can install or configure:

- Traefik / Gateway API routing
- cert-manager, when enabled by the target values
- metrics-server, when enabled by the target values
- PostgreSQL
- RustFS / S3-compatible object storage
- Authentik
- TAMOSS API, UI, and worker deployments

Use the site overlay to decide which cluster services TAMOSS owns. If the target
cluster already provides ingress, certificates, metrics, database, identity, or
object storage, point the chart values at those services rather than installing
duplicates.

## Naming Conventions

Examples in this document use these shell variables:

```bash
export KUBECONFIG=/path/to/remote.kubeconfig
export DEPLOY_VALUES_FILE=/path/to/site-values.yaml
export DOMAIN=tamoss.example.com
export TAMS_NAMESPACE=tams
export AUTH_NAMESPACE=authentik
export IMAGE_TAG=sha-xxxxxxx
```

If your site overlay uses different namespaces, set `TAMS_NAMESPACE` and
`AUTH_NAMESPACE` to match the deployed resources.

Expected public hostnames are:

| Service | URL |
| --- | --- |
| TAMOSS UI | `https://app.${DOMAIN}` |
| TAMOSS API | `https://api.${DOMAIN}` |
| API docs | `https://api.${DOMAIN}/docs` |
| Authentik | `https://auth.${DOMAIN}` |
| OAuth2 token endpoint | `https://auth.${DOMAIN}/application/o/token/` |
| S3 / RustFS API | `https://s3.${DOMAIN}` |

## Cluster Prerequisites

The operator workstation needs:

- `kubectl`
- `helm`
- `helmfile`
- `task`

The remote cluster needs:

- Kubernetes API access for the supplied kubeconfig
- a LoadBalancer or ingress path for public traffic
- Gateway API support when using HTTPRoutes
- DNS for `app`, `api`, `auth`, and `s3`
- a TLS issuer or pre-created certificates
- a suitable StorageClass for stateful services
- NetworkPolicy support if network policies are enabled

## Site Values Overlay

Review these values for every production target:

- public hostnames and cookie domain
- GatewayClass, Gateway, HTTPRoute, and TLS ownership
- whether cert-manager, Traefik, and metrics-server are installed by TAMOSS
- namespaces for TAMOSS and Authentik
- storage class and PVC sizing
- PostgreSQL credential source
- RustFS credential source and bucket settings
- Authentik bootstrap and OAuth client secret source
- API token source
- OAuth issuer, JWKS URI, and required scopes
- CORS allowed origins
- API, UI, and worker replica counts
- resource requests and limits
- autoscaling and PodDisruptionBudget settings
- NetworkPolicy ingress and egress rules

Keep site overlays private if they contain explicit credentials. Prefer
`existingSecret` references for production.

## Validate And Render

Validate the chart and Helmfile render paths:

```bash
task deploy:validate
```

Render the remote manifests without applying them:

```bash
task deploy:template \
  DEPLOY_ENV=remote \
  KUBECONFIG="$KUBECONFIG" \
  DEPLOY_VALUES_FILE="$DEPLOY_VALUES_FILE" \
  > /tmp/tamoss-remote-rendered.yaml
```

If the Helm diff plugin is available, review the remote diff:

```bash
task deploy:diff \
  DEPLOY_ENV=remote \
  KUBECONFIG="$KUBECONFIG" \
  DEPLOY_VALUES_FILE="$DEPLOY_VALUES_FILE"
```

## Deploy Remote

Deploy the remote target with explicit image tags:

```bash
task deploy:remote \
  KUBECONFIG="$KUBECONFIG" \
  DEPLOY_VALUES_FILE="$DEPLOY_VALUES_FILE" \
  API_IMAGE_TAG="$IMAGE_TAG" \
  UI_IMAGE_TAG="$IMAGE_TAG"
```

Use the same tag for API and UI unless deliberately testing a split release.
CI-built image tags normally use the form `sha-<7-char commit>`.

If Authentik readiness is skipped with a message about
`tests/targets/remote.env`, the deploy may still have applied successfully. That
message means the local readiness helper could not find the optional remote E2E
target file.

## Rollback

Rollback is a redeploy with the previous known-good image tags:

```bash
export IMAGE_TAG=sha-previous

task deploy:remote \
  KUBECONFIG="$KUBECONFIG" \
  DEPLOY_VALUES_FILE="$DEPLOY_VALUES_FILE" \
  API_IMAGE_TAG="$IMAGE_TAG" \
  UI_IMAGE_TAG="$IMAGE_TAG"
```

After rollback, verify rollouts and smoke tests before declaring recovery.

## Credentials

The charts support three production credential models:

- externally managed Kubernetes secrets via `existingSecret`
- explicit private values in the site overlay
- chart-generated in-cluster secrets, preserved across upgrades by Helm lookup

Default secret names and keys are:

| Purpose | Secret | Key |
| --- | --- | --- |
| Authentik bootstrap password | `tams-authentik` | `AUTHENTIK_BOOTSTRAP_PASSWORD` |
| OAuth client secret | `tams-authentik` | `TAMOSS_OAUTH_CLIENT_SECRET` |
| TAMOSS API token | `tams-api-token` | `TAMOSS_API_TOKEN` |
| PostgreSQL username | `tams-postgresql-auth` | `username` |
| PostgreSQL password | `tams-postgresql-auth` | `password` |
| PostgreSQL database | `tams-postgresql-auth` | `database` |
| RustFS access key | `tams-rustfs-auth` | `RUSTFS_ACCESS_KEY` |
| RustFS secret key | `tams-rustfs-auth` | `RUSTFS_SECRET_KEY` |

Retrieve a single secret key:

```bash
kubectl --kubeconfig "$KUBECONFIG" -n "$TAMS_NAMESPACE" \
  get secret tams-api-token \
  -o jsonpath='{.data.TAMOSS_API_TOKEN}' | base64 --decode; echo
```

Retrieve the Authentik bootstrap password:

```bash
kubectl --kubeconfig "$KUBECONFIG" -n "$AUTH_NAMESPACE" \
  get secret tams-authentik \
  -o jsonpath='{.data.AUTHENTIK_BOOTSTRAP_PASSWORD}' | base64 --decode; echo
```

Retrieve the OAuth client secret:

```bash
kubectl --kubeconfig "$KUBECONFIG" -n "$AUTH_NAMESPACE" \
  get secret tams-authentik \
  -o jsonpath='{.data.TAMOSS_OAUTH_CLIENT_SECRET}' | base64 --decode; echo
```

Retrieve RustFS credentials:

```bash
kubectl --kubeconfig "$KUBECONFIG" -n "$TAMS_NAMESPACE" \
  get secret tams-rustfs-auth \
  -o jsonpath='{.data.RUSTFS_ACCESS_KEY}' | base64 --decode; echo

kubectl --kubeconfig "$KUBECONFIG" -n "$TAMS_NAMESPACE" \
  get secret tams-rustfs-auth \
  -o jsonpath='{.data.RUSTFS_SECRET_KEY}' | base64 --decode; echo
```

## Admin UI Access

TAMOSS UI:

- URL: `https://app.${DOMAIN}`
- protected by Authentik forward-auth in the full remote profile

API docs:

- URL: `https://api.${DOMAIN}/docs`
- use bearer token or OAuth2 credentials for protected API calls

Authentik:

- URL: `https://auth.${DOMAIN}`
- bootstrap username: `akadmin`
- bootstrap password: `tams-authentik/AUTHENTIK_BOOTSTRAP_PASSWORD`

RustFS:

- public S3 API URL: `https://s3.${DOMAIN}`
- access key: `tams-rustfs-auth/RUSTFS_ACCESS_KEY`
- secret key: `tams-rustfs-auth/RUSTFS_SECRET_KEY`
- console access is not assumed; expose it only when the site overlay explicitly
  enables and protects it

Traefik:

- public traffic is handled through the Gateway/HTTPRoute path
- dashboard access is not assumed; expose it only when explicitly enabled and
  protected by cluster policy

## OAuth2 And External Clients

Default machine-client values:

- API endpoint: `https://api.${DOMAIN}`
- token endpoint: `https://auth.${DOMAIN}/application/o/token/`
- client ID: `tams-api-client`
- client secret: `tams-authentik/TAMOSS_OAUTH_CLIENT_SECRET`
- optional scopes: `tams-api/admin tams-api/read tams-api/write tams-api/delete`

Request a token:

```bash
CLIENT_SECRET="$(
  kubectl --kubeconfig "$KUBECONFIG" -n "$AUTH_NAMESPACE" \
    get secret tams-authentik \
    -o jsonpath='{.data.TAMOSS_OAUTH_CLIENT_SECRET}' | base64 --decode
)"

curl -fsS \
  -d grant_type=client_credentials \
  -d client_id=tams-api-client \
  -d client_secret="$CLIENT_SECRET" \
  -d "scope=tams-api/admin tams-api/read tams-api/write tams-api/delete" \
  "https://auth.${DOMAIN}/application/o/token/"
```

Discover the configured storage backends:

```bash
TOKEN="<access-token>"

curl -fsS \
  -H "Authorization: Bearer ${TOKEN}" \
  "https://api.${DOMAIN}/service/storage-backends"
```

External editors normally need:

- API endpoint
- OAuth2 token endpoint
- client ID and secret
- required scopes, if the site enforces scopes
- storage backend ID from `/service/storage-backends`
- browser-reachable segment URL label / public storage endpoint

## Scaling

Horizontally safe components:

- API
- UI
- worker

Components requiring a dedicated plan:

- PostgreSQL
- RustFS in standalone mode
- Authentik and its database

Prefer scaling through the site overlay so the desired state is repeatable:

```yaml
tams:
  api:
    replicaCount: 3
  ui:
    replicaCount: 3
  worker:
    replicaCount: 3
```

If autoscaling is enabled for API or UI, set the replica bounds in values:

```yaml
tams:
  api:
    autoscaling:
      enabled: true
      minReplicas: 2
      maxReplicas: 6
      targetCPUUtilizationPercentage: 80
```

For an emergency manual scale, use labels rather than assuming release names:

```bash
kubectl --kubeconfig "$KUBECONFIG" -n "$TAMS_NAMESPACE" \
  scale deployment \
  -l app.kubernetes.io/component=api \
  --replicas=3
```

Manual scaling is temporary. Update the values overlay and redeploy to make it
the durable state.

## Day-2 Operations

List pods:

```bash
kubectl --kubeconfig "$KUBECONFIG" -n "$TAMS_NAMESPACE" get pods -o wide
kubectl --kubeconfig "$KUBECONFIG" -n "$AUTH_NAMESPACE" get pods -o wide
```

Check rollouts:

```bash
kubectl --kubeconfig "$KUBECONFIG" -n "$TAMS_NAMESPACE" \
  rollout status deployment -l app.kubernetes.io/component=api

kubectl --kubeconfig "$KUBECONFIG" -n "$TAMS_NAMESPACE" \
  rollout status deployment -l app.kubernetes.io/component=ui

kubectl --kubeconfig "$KUBECONFIG" -n "$TAMS_NAMESPACE" \
  rollout status deployment -l app.kubernetes.io/component=worker
```

Tail logs:

```bash
kubectl --kubeconfig "$KUBECONFIG" -n "$TAMS_NAMESPACE" \
  logs -l app.kubernetes.io/component=api --tail=100 -f

kubectl --kubeconfig "$KUBECONFIG" -n "$TAMS_NAMESPACE" \
  logs -l app.kubernetes.io/component=worker --tail=100 -f

kubectl --kubeconfig "$KUBECONFIG" -n "$TAMS_NAMESPACE" \
  logs -l app.kubernetes.io/component=ui --tail=100 -f
```

Inspect routing:

```bash
kubectl --kubeconfig "$KUBECONFIG" get gatewayclass
kubectl --kubeconfig "$KUBECONFIG" -n "$TAMS_NAMESPACE" get gateway,httproute
kubectl --kubeconfig "$KUBECONFIG" -n "$TAMS_NAMESPACE" describe httproute
```

Restart a component:

```bash
kubectl --kubeconfig "$KUBECONFIG" -n "$TAMS_NAMESPACE" \
  rollout restart deployment -l app.kubernetes.io/component=api
```

## Storage Operations

Default storage shape:

- internal S3 endpoint: service DNS inside the cluster
- public S3 endpoint: `https://s3.${DOMAIN}`
- bucket: `tams`, unless overridden by site values
- credentials: `tams-rustfs-auth`

Browser uploads and browser media reads require the public S3 endpoint to be
reachable from user clients and to allow the required CORS methods and headers.
At minimum, allow the trusted UI origin, `Authorization`, `Content-Type`, and
the methods used by presigned upload/read flows.

RustFS standalone mode is one pod with persistent storage. Treat it as a simple
single-instance object store. Production requirements for high availability,
backup, retention, and disaster recovery should be handled explicitly by the
site operator.

## Database Operations

Default database shape:

- PostgreSQL runs in the TAMOSS namespace unless overridden
- credentials: `tams-postgresql-auth`
- schema source: `db/schema.sql`
- bootstrap source: `db/bootstrap.sql`

Port-forward for controlled maintenance:

```bash
kubectl --kubeconfig "$KUBECONFIG" -n "$TAMS_NAMESPACE" \
  port-forward svc/tams-stack-postgresql 5432:5432
```

If your release name changes, the PostgreSQL service name may differ. Prefer
discovering it first:

```bash
kubectl --kubeconfig "$KUBECONFIG" -n "$TAMS_NAMESPACE" get svc \
  -l app.kubernetes.io/name=postgresql
```

Backups and restores are operator responsibilities. Do not rely on chart
releases as database backups.

## Networking And Security

Remote deployments should keep these boundaries clear:

- UI traffic is browser-oriented and Authentik protected.
- Direct API traffic is protected by TAMOSS bearer-token / OAuth2 validation.
- S3 traffic is public only to the extent required for presigned object access.
- CORS should list trusted browser origins; wildcard CORS should be a deliberate
  exception, not the default production stance.
- NetworkPolicy ingress isolation is enabled by the remote profile, but egress
  may remain open unless tightened by site values.

When tightening egress, account for:

- Kubernetes DNS
- PostgreSQL
- object storage
- Authentik/JWKS/token endpoints
- webhook destinations
- external monitoring or logging sinks

## Remote E2E Target

The deployed test suite can run against a remote cluster when
`tests/targets/remote.env` exists. The file is ignored by git.

Start from the example:

```bash
cp tests/targets/remote.env.example tests/targets/remote.env
```

Set:

- `TEST_TAMOSS_API=https://api.${DOMAIN}`
- `TEST_TAMOSS_UI=https://app.${DOMAIN}`
- `TEST_TAMOSS_AUTH=https://auth.${DOMAIN}`
- one API auth method, either `TEST_TAMOSS_TOKEN` or basic auth fields
- UI/Auth credentials for browser tests
- `KUBECONFIG`, if you want tests to retrieve secrets from the cluster

Run:

```bash
task e2e:deployed \
  DEPLOY_ENV=remote \
  KUBECONFIG="$KUBECONFIG"
```

## Smoke Tests

Check public endpoints:

```bash
curl -fsS "https://api.${DOMAIN}/docs" >/dev/null
curl -fsS "https://auth.${DOMAIN}/application/o/tams-api/jwks/" >/dev/null
curl -fsS -o /dev/null -w '%{http_code}\n' "https://app.${DOMAIN}/"
curl -fsS -o /dev/null -w '%{http_code}\n' "https://s3.${DOMAIN}/"
```

Expected UI status may be `302` when Authentik redirects unauthenticated users.
Expected S3 status depends on the storage service and route configuration; a
non-empty HTTP response is enough for a basic reachability check.

Check authenticated API access:

```bash
TOKEN="$(
  kubectl --kubeconfig "$KUBECONFIG" -n "$TAMS_NAMESPACE" \
    get secret tams-api-token \
    -o jsonpath='{.data.TAMOSS_API_TOKEN}' | base64 --decode
)"

curl -fsS \
  -H "Authorization: Bearer ${TOKEN}" \
  "https://api.${DOMAIN}/service"
```

## Change Procedure

Before a production change:

1. Confirm CI is green for the commit to deploy.
2. Record the current API and UI image tags.
3. Render or diff the remote manifests.
4. Confirm DNS/TLS and any secret changes are already present.

Apply:

1. Deploy with explicit `API_IMAGE_TAG` and `UI_IMAGE_TAG`.
2. Watch rollouts for API, UI, worker, Authentik, PostgreSQL, and RustFS.
3. Run smoke tests.
4. Run deployed E2E when the change affects API, auth, routing, storage, or UI.

Rollback:

1. Redeploy the previous image tags.
2. Watch rollouts.
3. Re-run smoke tests.
4. Record the incident, deployed tag, and any follow-up action.
