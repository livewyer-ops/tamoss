# Secret Rotation

TAMOSS runtimes use the current operator-rendered environment contract for
database and default S3 credentials. File-backed runtime secrets are kept only
where the application intentionally reloads them during request or worker
processing.

| Secret class | Runtime path | Rotation behavior |
| --- | --- | --- |
| API token | `TAMOSS_API_TOKEN_FILE` | Read on each authenticated request. |
| Basic auth password | `TAMOSS_BASIC_AUTH_PASSWORD_FILE` | Read on each Basic auth request. |
| Database credentials | `POSTGRES_*` environment | Startup-only; rotate through the operator and roll pods. |
| Default S3 credentials | `TAMOSS_S3_ACCESS_KEY`, `TAMOSS_S3_SECRET_KEY` | Startup-only; rotate through the operator and roll pods. |
| Additional StorageBackend credentials | `TAMOSS_STORAGE_BACKEND_CREDENTIALS_FILE` | Reloaded when the credential file changes. |
| OAuth bearer validation keys | JWKS URL | Provider-owned; TAMOSS uses JWKS caching settings. |
| TLS certificates | cert-manager or external TLS provider | Provider-owned. |

Kubernetes installations should rotate referenced Secrets through the `Tamoss`
CR or provider-owned Secret, then let the operator roll the affected workloads.

The UI must only receive public runtime configuration. Sensitive OAuth client
credentials, API tokens, database credentials, and S3 credentials stay in API or
worker server-side runtime files.

## Secret Sources

Create Secrets in the namespace that contains the `Tamoss` CR unless a field
explicitly points at a platform namespace, such as an Authentik API token
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
| Database credentials | `POSTGRES_*` env | Startup-only | Update the referenced Secret and roll API, worker, and schema-migration pods through the operator. |
| S3 default backend credentials | Static env from the referenced Secret | Startup-only | Update the referenced Secret and roll API and worker pods through the operator. |
| Additional StorageBackend credentials | Mounted storage-backend credentials JSON | Reloaded when the file mtime changes; the last good parse is kept during partial updates. | Update the projected Secret file. |
| OAuth2 verifier config | OAuth2 issuer, JWKS URI, algorithms, audience, and scope env | Startup-only | Update the `Tamoss` CR or referenced Secret and roll API/worker pods through the operator. |
| Forward-auth proof secret | `TAMOSS_FORWARD_AUTH_SHARED_SECRET_FILE` or static env | Hot-reloaded when file-backed; static env values are startup-only | Update the projected Secret file for file-backed values. Restart or roll the pod for static env values. |
| Webhook endpoint credentials | Stored webhook registration data | Hot-resolved from the current webhook record before delivery | Update the webhook registration. Pending deliveries use the current live credential value. |
| TLS material | Ingress, Gateway, cert-manager, or provider-managed Secrets | Provider-owned and outside the Python runtime | Rotate through the ingress/provider controller. TAMOSS runtime pods do not read TLS key material directly. |

Webhook delivery records store a credential reference instead of the raw
`api_key_value`. The worker resolves the live webhook credential immediately
before sending the request.
