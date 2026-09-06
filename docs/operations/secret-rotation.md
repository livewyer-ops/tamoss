# Secret Rotation

TAMOSS runtimes use the current operator-rendered environment contract for
database and default S3 credentials. File-backed runtime secrets are kept only
where the application intentionally reloads them during request or worker
processing.

| Secret class | Runtime path | Rotation behaviour |
| --- | --- | --- |
| API token | `TAMOSS_API_TOKEN_FILE` | Read on each authenticated request. |
| Basic auth password | `TAMOSS_BASIC_AUTH_PASSWORD_FILE` | Read on each Basic auth request. |
| Database credentials | `POSTGRES_*` environment | Startup-only; rotate through the database provider and explicitly roll API/worker pods. |
| Static S3 credentials | `TAMOSS_S3_ACCESS_KEY`, `TAMOSS_S3_SECRET_KEY` | Startup-only if no credential-file override is present. |
| Operator-managed StorageBackend credentials, including the default backend | `TAMOSS_STORAGE_BACKEND_CREDENTIALS_FILE` | Reloaded when the rendered credential file changes; takes precedence over static credentials. |
| OAuth bearer validation keys | JWKS URL | Provider-owned; TAMOSS uses JWKS caching settings. |
| Forward-auth proofs | `<instance>-forward-auth` Secret keys `api-proof` and `console-proof` | Operator-owned. UI receives both issuer proofs; API and Console receive only their own verifier proof. |
| TLS certificates | [cert-manager](https://cert-manager.io/) or external TLS provider | Provider-owned. |

Updating data in the same-named database Secret does not change the pod template
and does not trigger an automatic rollout. After updating the provider-owned
credential and its referenced Secret, explicitly restart the API and worker as
described below. Reconciliation alone is not evidence that existing pods use new
environment values.

The UI must only receive public runtime configuration. Sensitive OAuth client
credentials, API tokens, database credentials, and S3 credentials stay in API or
worker server-side runtime configuration.

## Secret Sources

Create Secrets in the namespace that contains the `Tamoss` CR unless a field
explicitly points at a platform namespace, such as an
[Authentik](https://goauthentik.io/) API token
reference.

Do not commit real Secret values. Use your cluster's normal secret-management
path for production.

When an environment variable names a secret file, TAMOSS treats that as an
explicit runtime contract. The API, worker, and migration command fail closed if
the file cannot be read instead of silently falling back to defaults. This
applies to `TAMOSS_API_TOKEN_FILE`, `TAMOSS_BASIC_AUTH_PASSWORD_FILE`, and
`TAMOSS_FORWARD_AUTH_SHARED_SECRET_FILE`.

## Reload Policy

| Secret class | Source | Reload policy | Rotation action |
| --- | --- | --- | --- |
| API token and basic-auth password | `TAMOSS_API_TOKEN_FILE`, `TAMOSS_BASIC_AUTH_PASSWORD_FILE`, or static env | Hot-reloaded when file-backed; static env values are startup-only | Update the projected Secret file for file-backed values. Restart or roll the pod for static env values. |
| Database credentials | `POSTGRES_*` env | Startup-only | Update the provider credential and referenced Secret, then explicitly roll API and worker. Existing migration Jobs do not reload environment values. |
| Static S3 credentials without a file override | Static env from the referenced Secret | Startup-only | Update the referenced Secret, then explicitly roll API and worker. |
| Operator-managed StorageBackend credentials, including the default backend | Mounted storage-backend credentials JSON | Reloaded when the file mtime changes; the last good parse is kept during partial updates. | Update the referenced backend Secret and wait for reconciliation and projection; verify a new storage operation before revoking old credentials. |
| OAuth2 verifier config | OAuth2 issuer, JWKS URI, algorithms, audience, and scope env | Startup-only | Update the `Tamoss` CR or referenced Secret and roll API/worker pods through the operator. |
| Forward-auth proof secret | Operator-generated `<instance>-forward-auth` Secret mounted as files | API reads `api-proof` per request; UI loads both proofs and Console loads `console-proof` at startup. Separate checksums roll only each proof's issuer and verifier. | Replace one key to contain that trust domain, or delete the Secret to replace both. Projection and rollout are fail-closed but not atomic, so a brief `401` or `503` is possible while the issuer and verifier converge. |
| Webhook endpoint credentials | Stored webhook registration data | Hot-resolved from the current webhook record before delivery | Update the webhook registration. Pending deliveries use the current live credential value. |
| TLS material | Ingress, Gateway, cert-manager, or provider-managed Secrets | Provider-owned and outside the Python runtime | Rotate through the ingress/provider controller. TAMOSS runtime pods do not read TLS key material directly. |

Webhook delivery records store a credential reference instead of the raw
`api_key_value`. The worker resolves the live webhook credential immediately
before sending the request.

## Database Rotation

First rehearse the provider's rotation procedure in an isolated environment.
Prefer overlapping old and new credentials where the provider permits it;
otherwise schedule the expected connection interruption. Avoid rotation during
schema migration and do not restart a completed migration Job merely to reload
credentials.

1. Confirm the Kubernetes context, namespace and instance. Identify API and
   worker Deployment names from labels rather than assuming names when
   `fullnameOverride` is used.
2. Update the database credential through its owning provider and update the
   referenced Kubernetes Secret through the normal secret-management path.
   Do not print credentials or place them in shell history.
3. Explicitly restart each affected Deployment, using the names confirmed above:

```bash
kubectl --context "$CONTEXT" -n "$NAMESPACE" get deployments \
  -l "app.kubernetes.io/instance=$INSTANCE" -L app.kubernetes.io/component
kubectl --context "$CONTEXT" -n "$NAMESPACE" rollout restart \
  "deployment/$API_DEPLOYMENT" "deployment/$WORKER_DEPLOYMENT"
kubectl --context "$CONTEXT" -n "$NAMESPACE" rollout status \
  "deployment/$API_DEPLOYMENT" --timeout=300s
kubectl --context "$CONTEXT" -n "$NAMESPACE" rollout status \
  "deployment/$WORKER_DEPLOYMENT" --timeout=300s
```

4. Confirm replacement pod UIDs and successful database-backed API reads and
   worker processing. Readiness alone is insufficient to prove delivery to an
   external client; use the [Cutting Rooms acceptance checks](cutting-rooms.md)
   where that integration is in use.
5. Revoke old credentials only after the checks succeed. If they fail, retain
   or restore the previous provider credential and Secret and repeat the
   rollout; do not remove the only working credential first.

Record the tested revision, deployment names, replacement pod UIDs and outcomes,
without secret values. This runbook is an operational procedure, not evidence
that a particular deployment has completed rotation.
