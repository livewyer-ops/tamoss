# Troubleshooting

Use this guide when a TAMOSS deployment does not become ready or a running
instance starts failing. Start with read-only checks. Avoid deleting pods,
editing Secrets, or patching the `Tamoss` CR until the failing layer is clear.

## Redaction

Before sharing diagnostics publicly, remove:

- Secret values, bearer tokens, OAuth client secrets, private keys, database
  passwords, S3 access keys, and Authentik API tokens.
- Complete presigned S3 URLs.
- Private hostnames or IP addresses that should not be disclosed.

Useful public issue details are profile name, Kubernetes version, TAMOSS image
tags, condition summaries, Events, provider ownership choices, and redacted
logs.

## First Checks

```bash
export KUBECONFIG=tams.kubeconfig
export TAMOSS_NAMESPACE=tams
export TAMOSS_NAME=tamoss-kind

kubectl --kubeconfig "$KUBECONFIG" -n "$TAMOSS_NAMESPACE" get tamoss "$TAMOSS_NAME" -o wide
kubectl --kubeconfig "$KUBECONFIG" -n "$TAMOSS_NAMESPACE" describe tamoss "$TAMOSS_NAME"
kubectl --kubeconfig "$KUBECONFIG" -n "$TAMOSS_NAMESPACE" get pods,svc,ingress
kubectl --kubeconfig "$KUBECONFIG" -n "$TAMOSS_NAMESPACE" get httproute
kubectl --kubeconfig "$KUBECONFIG" -n "$TAMOSS_NAMESPACE" get events --sort-by=.lastTimestamp
kubectl --kubeconfig "$KUBECONFIG" -n tamoss-system logs deploy/operator-controller-manager --tail=200
```

If Gateway API CRDs are not installed, the `httproute` query can fail. In that
case, inspect `RoutingReady` and `HostnamesReady` on the `Tamoss` resource and
use the Gateway API section below only when `spec.httpRoute.enabled=true`.

Read `Tamoss` conditions first:

| Condition | Typical area |
| --- | --- |
| `BackendsReady=False` | Database, S3, Authentik, or prerequisite CRDs. |
| `BackupPolicyReady=False` | Managed CNPG backup policy or archiving health. |
| `SchemaMigrated=False` | PostgreSQL or schema migration Job. |
| `IdentityBlueprintSubmitted=False` | Managed Authentik blueprint submission. |
| `IdentityReady=False` | Authentik or external OAuth/OIDC. |
| `RoutingReady=False` | Ingress or Gateway API routing resources. |
| `HostnamesReady=False` | Ingress or Gateway API hostname admission. |
| `Progressing=True` | Rollout still in progress. |
| `Degraded=True` | User-actionable reconcile failure. |
| `Paused=True` | `.spec.paused=true`; writes are suspended. |

Then inspect provider ownership:

```bash
kubectl --kubeconfig "$KUBECONFIG" -n "$TAMOSS_NAMESPACE" \
  get tamoss "$TAMOSS_NAME" -o jsonpath='{.status.providers}'
```

`ownership: managed` means TAMOSS owns the instance resource after its
prerequisite platform capability exists. `ownership: external` means TAMOSS
uses the supplied configuration and does not mutate the external service.

## First-Start Lifecycle

Treat first start as one product lifecycle rather than disconnected pods:

| Phase | Status source | Healthy state |
| --- | --- | --- |
| Dependencies and provider backends | `Tamoss.status.conditions[BackendsReady]` | `True`, or a clear missing dependency/provider reason. |
| Default storage bucket | default `StorageBackend.status.conditions[BucketReady]` | `True`; external or unmanaged storage is reported through provider ownership. |
| Schema migration | `Tamoss.status.conditions[SchemaMigrated]` | `True` with the desired schema version in status. |
| Storage database registration | `StorageBackend.status.conditions[DatabaseReady]` | `True` after the TAMS storage backend row is registered. |
| Identity | `Tamoss.status.conditions[IdentityReady]` | `True`, `ExternalIdentityConfigured`, or not required when auth is disabled. |
| Workloads | `Tamoss.status.replicas.{api,ui,worker}` | available replicas match desired replicas for enabled components. |
| Routes | `Tamoss.status.conditions[RoutingReady]` | `True`, or external routing ownership when routes are not managed. |

`task kind:summary` and the support bundle read these phases from Kubernetes
status. When first start stops, inspect the first phase that is not ready,
skipped, or externally managed before restarting pods or changing the CR.

If Prometheus scraping is enabled, use the operator metrics as a pointer back
to the same status surface:

- `tamoss_resource_condition{condition="Ready",status!="True"}` identifies
  resources that have not become ready.
- `tamoss_resource_condition{condition="Degraded",status="True"}` identifies
  resources with user-actionable failures.
- `tamoss_provider_ready == 0` identifies the provider domain to inspect next:
  `db`, `s3`, `auth`, or `routing`.
- `tamoss_reconcile_errors_total` and `tamoss_reconcile_duration_seconds`
  identify controller-level failures or latency.

## API Startup And Readiness

The API separates process health from dependency readiness:

| Class | Signal | Meaning |
| --- | --- | --- |
| Fatal configuration | Pod does not start, or startup logs mention `StartupConfigurationError`. | Required settings such as database URL or S3 backend configuration are invalid. Fix the `Tamoss` CR or referenced Secret. |
| Dependency readiness | `/healthz` returns `200`; `/readyz` returns `503` with `DatabaseUnavailable`, `SchemaRevisionMultipleHeads`, `SchemaRevisionMismatch`, `SchemaRevisionUnsupported`, `StorageBackendMetadataUnavailable`, `StorageBackendMetadataMissing`, or `ObjectStoreUnreachable`. | The process is healthy, but PostgreSQL, migration state, operator-registered storage metadata, or object-store reachability is not ready yet. Inspect `BackendsReady`, `SchemaMigrated`, and the default `StorageBackend` conditions. |
| Unexpected runtime failure | API responses return `type=internal_server_error` with an `incident_id`. | Treat as an application bug or unexpected platform failure. Search pod logs for the incident ID to find the concrete exception detail, then collect a support bundle. |

Readiness checks are read-only. They query database and storage metadata state
and perform a lightweight object-store bucket check. They do not create schema,
create buckets, register storage backends, write media, or mutate queue state.

## Platform

```bash
kubectl --kubeconfig "$KUBECONFIG" -n cert-manager get pods
kubectl --kubeconfig "$KUBECONFIG" -n traefik get pods,svc
kubectl --kubeconfig "$KUBECONFIG" -n auth get pods
kubectl --kubeconfig "$KUBECONFIG" get crd tamosses.tamoss.livewyer.io storagebackends.tamoss.livewyer.io
```

For managed backends:

```bash
kubectl --kubeconfig "$KUBECONFIG" get crd clusters.postgresql.cnpg.io tenants.rustfs.com
kubectl --kubeconfig "$KUBECONFIG" -n cnpg-system get pods
kubectl --kubeconfig "$KUBECONFIG" -n rustfs-system get pods
```

If a managed provider is selected but its CRD is missing, the operator reports
`MissingDependencyOperator` and names the missing dependency in the
`BackendsReady` condition message. Install the prerequisite through your
administrator path or the checked-in platform bootstrap, then reconcile again.

## Application Pods

Pending pods usually indicate PVC, StorageClass, resource, node selector,
toleration, affinity, quota, or limit-range problems.

CrashLooping pods usually indicate Secret, database, S3, OAuth, or application
configuration problems.

```bash
kubectl --kubeconfig "$KUBECONFIG" -n "$TAMOSS_NAMESPACE" describe pod <pod-name>
kubectl --kubeconfig "$KUBECONFIG" -n "$TAMOSS_NAMESPACE" logs <pod-name> --previous
```

Worker pods use an exec probe that runs the same dependency check available
from the container:

```bash
WORKER_DEPLOYMENT="$(kubectl --kubeconfig "$KUBECONFIG" -n "$TAMOSS_NAMESPACE" \
  get tamoss "$TAMOSS_NAME" -o jsonpath='{.status.resolved.resources.worker}')"
kubectl --kubeconfig "$KUBECONFIG" -n "$TAMOSS_NAMESPACE" \
  exec deploy/"$WORKER_DEPLOYMENT" -- /bin/uv run python -m tamoss.worker health

kubectl --kubeconfig "$KUBECONFIG" -n "$TAMOSS_NAMESPACE" get tamoss "$TAMOSS_NAME" \
  -o jsonpath='{.status.replicas.worker}'
```

The worker health command validates runtime configuration, PostgreSQL
connectivity, and the mounted StorageBackend credentials file. It does not claim
queue work, send webhooks, delete media, or register storage metadata.

## API or UI Fails

```bash
curl -k https://api.tamoss.localtest.me/healthz
curl -k https://api.tamoss.localtest.me/readyz
kubectl --kubeconfig "$KUBECONFIG" -n "$TAMOSS_NAMESPACE" \
  logs -l app.kubernetes.io/component=api --tail=100
```

For remote clusters, use the public API hostname configured in the `Tamoss` CR.

## Support Bundle

Generate a local diagnostic bundle before opening a public issue or handing off
an incident:

```bash
task operator:support-bundle \
  KUBECONFIG="$KUBECONFIG" \
  TAMOSS_NAMESPACE="$TAMOSS_NAMESPACE" \
  TAMOSS_NAME="$TAMOSS_NAME"
```

The bundle includes `Tamoss`, `StorageBackend`, CNPG backup resources, workload
objects, routes, events, CRDs, webhook configuration, current logs, and previous
container logs when Kubernetes has them. Operator namespace objects and events
are collected separately from instance namespace objects. `bundle.json`
summarizes the collected `Tamoss` schema, resolved runtime version fields, and
first-start lifecycle phases so version and startup state are visible without
opening the full resource dump first.

Secret values, sensitive ConfigMaps such as `oauth2-credentials`, direct token
fields, webhook API-key values, authorization header values, credential-bearing
URLs, last-applied annotations, and known credential-bearing environment values
are redacted. Generated Secret names from `status.resolved.*` remain visible so
references can be diagnosed, but generated Secret bodies are not shared. The
command writes to the local filesystem only and does not upload diagnostics.

## PostgreSQL

For managed CNPG:

```bash
kubectl --kubeconfig "$KUBECONFIG" -n "$TAMOSS_NAMESPACE" get clusters.postgresql.cnpg.io
kubectl --kubeconfig "$KUBECONFIG" -n "$TAMOSS_NAMESPACE" describe cluster "${TAMOSS_NAME}-db"
kubectl --kubeconfig "$KUBECONFIG" -n "$TAMOSS_NAMESPACE" get scheduledbackups.postgresql.cnpg.io
kubectl --kubeconfig "$KUBECONFIG" -n "$TAMOSS_NAMESPACE" get tamoss "$TAMOSS_NAME" \
  -o jsonpath='{range .status.conditions[?(@.type=="BackupPolicyReady")]}{.status}{" "}{.reason}{" - "}{.message}{"\n"}{end}'
kubectl --kubeconfig "$KUBECONFIG" -n "$TAMOSS_NAMESPACE" get jobs,pods \
  -l tamoss.livewyer.io/schema-migration=true
```

For external PostgreSQL:

- Confirm host, port, database, Secret name, and Secret key names.
- Confirm NetworkPolicy permits API, worker, and migration pods to connect.
- Confirm the migration role can create and update schema objects.

## S3 and StorageBackend

For managed RustFS Operator:

```bash
DEFAULT_STORAGEBACKEND="$(kubectl --kubeconfig "$KUBECONFIG" -n "$TAMOSS_NAMESPACE" \
  get tamoss "$TAMOSS_NAME" -o jsonpath='{.status.resolved.resources.defaultStorageBackend}')"
kubectl --kubeconfig "$KUBECONFIG" -n "$TAMOSS_NAMESPACE" get tenants.rustfs.com
kubectl --kubeconfig "$KUBECONFIG" -n "$TAMOSS_NAMESPACE" describe tenant "$TAMOSS_NAME"
kubectl --kubeconfig "$KUBECONFIG" -n "$TAMOSS_NAMESPACE" get storagebackend
kubectl --kubeconfig "$KUBECONFIG" -n "$TAMOSS_NAMESPACE" describe storagebackend "$DEFAULT_STORAGEBACKEND"
```

Managed RustFS bucket creation and CORS configuration are reconciled by the
operator through a native S3-compatible client. There is no bucket helper Job to
inspect; use `StorageBackend` conditions and Events for bucket readiness
failures.

The API `/readyz` response separates `storage_backends` metadata readiness from
`object_store` reachability. If `storage_backends` is ready but `object_store`
reports `ObjectStoreUnreachable`, inspect the `StorageBackend` `BucketReady`,
`DatabaseReady`, and external diagnostic conditions plus the mounted runtime
credentials Secret.

For local Kind, browser ingest uploads directly to
`https://s3.tamoss.localtest.me`. If the browser has accepted the app origin
but not the S3 origin, uploads can fail as a CORS or generic network error even
though the backend is healthy.

For external S3:

- Confirm the bucket exists.
- Confirm the endpoint and region match the provider.
- Confirm the referenced Secret and key names match the `StorageBackend`.
- Confirm the public endpoint is reachable from browsers.
- Confirm bucket CORS permits the TAMOSS UI origin, `PUT`, and `content-type`.

The operator records a best-effort `ExternalS3DiagnosticReady` condition on
external `StorageBackend` resources. `CORSMisconfigured`,
`EndpointUnreachable`, and `TLSValidationFailed` are diagnostic warnings; they
do not mutate external buckets and can be false positives or false negatives
because browser behavior, presigned URL shape, and provider CORS evaluation are
owned by the external service.

The API can allocate storage successfully while browser ingest still fails. If
the UI reports a CORS error, test the preflight against a fresh presigned URL
without sharing the URL publicly:

```bash
curl -sS -o /tmp/preflight-body.txt -D /tmp/preflight-headers.txt \
  -X OPTIONS "$PRESIGNED_PUT_URL" \
  -H 'Origin: https://app.tamoss.localtest.me' \
  -H 'Access-Control-Request-Method: PUT' \
  -H 'Access-Control-Request-Headers: content-type'

grep -i '^access-control' /tmp/preflight-headers.txt
```

For Backblaze B2, the web-console "share with every origin" setting can allow
`GET`/`HEAD` without allowing S3 `PUT`. Use B2 Native CORS rules when the bucket
already has native CORS configuration.

## TLS and Ingress

The local and reference profiles expose TAMOSS through Traefik on port 443.
They do not require application NodePorts or application port-forwarding.

```bash
kubectl --kubeconfig "$KUBECONFIG" -n cert-manager get pods
kubectl --kubeconfig "$KUBECONFIG" -n "$TAMOSS_NAMESPACE" get certificate,secret,ingress
kubectl --kubeconfig "$KUBECONFIG" -n "$TAMOSS_NAMESPACE" describe ingress
```

For local Kind, `*.tamoss.localtest.me` should resolve to `127.0.0.1`.
Local Kind certificates are self-signed and browser trust is per origin. Open
and accept or trust the local certificate warning for each browser-facing origin
you use: `app`, `api`, `s3`, and `auth`.

## Gateway API and HTTPRoute

TAMOSS can render `HTTPRoute` resources when `spec.httpRoute.enabled` is true,
but the operator does not install Gateway API or a Gateway controller.
The operator registers owned `HTTPRoute` watches when Gateway API is available
at manager startup. If Gateway API is installed after the operator starts,
restart the operator Deployment or wait for another `Tamoss` change to trigger
reconciliation.

```bash
kubectl --kubeconfig "$KUBECONFIG" -n "$TAMOSS_NAMESPACE" get httproute
kubectl --kubeconfig "$KUBECONFIG" -n "$TAMOSS_NAMESPACE" describe httproute
kubectl --kubeconfig "$KUBECONFIG" -n "$TAMOSS_NAMESPACE" describe tamoss "$TAMOSS_NAME"
```

If Gateway API CRDs are missing, `RoutingReady=False` and
`HostnamesReady=False` report `Reason=GatewayAPIUnavailable`. If a Gateway
controller rejects a route, `RoutingReady=False` reports the route reason. When
the rejection is hostname-related, `HostnamesReady=False` reports the Gateway
API conflict reason.

## Reapply or Reset

For an existing cluster, keep durable changes in the generated environment
overlay and reapply the supported workflow:

```bash
task env:apply ENV=my-prod KUBECONFIG="$KUBECONFIG"
task env:wait ENV=my-prod KUBECONFIG="$KUBECONFIG"
```

For a disposable local Kind cluster, recreate the full local environment:

```bash
task kind:up PROFILE=local-kind KUBECONFIG=tams.kubeconfig
task kind:e2e PROFILE=local-kind
task kind:down
```

`task kind:down` deletes the local Kind cluster and local runtime state. Do not
use it as a production reset path.
