# TAMOSS Configuration Guide

Configuration variables used by the TAMOSS API, worker, and deployment
profiles. Runtime-owned variables use the `TAMOSS_*` prefix.

## Database

| Variable | Default | Description |
|----------|---------|-------------|
| `TAMOSS_DATABASE_URL` | _(unset)_ | Preferred full PostgreSQL URL. Takes precedence over constructed URLs. |
| `DATABASE_URL` | _(unset)_ | Compatibility full PostgreSQL URL used when `TAMOSS_DATABASE_URL` is unset. |
| `POSTGRES_HOST` | _(unset)_ | PostgreSQL host used when no full database URL is provided. |
| `POSTGRES_PORT` | `5432` | PostgreSQL port used with `POSTGRES_HOST`. |
| `POSTGRES_DB` | `tams` | PostgreSQL database name used with `POSTGRES_HOST`. |
| `POSTGRES_USER` | `tams` | PostgreSQL username used with `POSTGRES_HOST`. |
| `POSTGRES_PASSWORD` | `tams` | PostgreSQL password used with `POSTGRES_HOST`. |
| `TAMOSS_DB_PASSWORD_FILE` | `/run/secrets/db_password`, `.local/db_password` | Optional password file. If present, it overrides `POSTGRES_PASSWORD`. |
| `TAMOSS_DATABASE_POOL_MIN_SIZE` | `1` | Minimum PostgreSQL connections kept by each API or worker process. |
| `TAMOSS_DATABASE_POOL_MAX_SIZE` | `10` | Maximum PostgreSQL connections used by each API or worker process. |

Runtime metadata is stored in PostgreSQL. Production and Kubernetes profiles
must provide PostgreSQL.

## Object Storage

TAMOSS builds one S3-compatible storage backend from environment variables.

| Variable | Default | Description |
|----------|---------|-------------|
| `TAMOSS_S3_ACCESS_KEY` | _(unset)_ | S3-compatible access key. Required with `TAMOSS_S3_SECRET_KEY` to enable the default S3 backend. |
| `TAMOSS_S3_SECRET_KEY` | _(unset)_ | S3-compatible secret key. |
| `TAMOSS_S3_ENDPOINT` | `http://localhost:9000` | Internal S3-compatible endpoint used by API and worker operations. |
| `TAMOSS_S3_PUBLIC_ENDPOINT` | same as `TAMOSS_S3_ENDPOINT` | Browser-facing endpoint used for object URLs and presigned URLs. |
| `TAMOSS_S3_BUCKET` | `tamoss` | Bucket name for the default S3-compatible backend. |
| `TAMOSS_S3_REGION` | `us-east-1` | Region recorded on the storage backend. |
| `TAMOSS_STORAGE_BACKEND_ID` | stable TAMOSS UUID | Storage backend UUID advertised in API responses. |
| `TAMOSS_STORAGE_LABEL` | `tamoss.<region>:s3:<bucket>` | Public backend label. |
| `TAMOSS_STORAGE_PROVIDER` | `tamoss` | Provider value exposed on verbose storage metadata. |
| `TAMOSS_S3_PRESIGN_TTL` | `3600` | Presigned URL lifetime in seconds. |
| `TAMOSS_S3_CONNECT_TIMEOUT_SECONDS` | `1` | S3 client connect timeout. |
| `TAMOSS_S3_READ_TIMEOUT_SECONDS` | `2` | S3 client read timeout. |
| `TAMOSS_S3_MAX_POOL_CONNECTIONS` | `40` | Maximum HTTP connections held by each reused boto3 S3 client. Keep this at least as large as the API threadpool size. |
| `TAMOSS_S3_AUTO_CREATE_BUCKET` | `false` | Lazily create the selected S3 bucket before allocation/write. Enabled by the Kind target; production buckets are normally provisioned separately. |

The Kind profile uses an internal RustFS endpoint for operations and
`https://s3.tamoss.localtest.me` as the browser-facing endpoint.

## Service Metadata

| Variable | Default | Description |
|----------|---------|-------------|
| `TAMOSS_AUTH_REQUIRED` | `false` | Require authentication for API requests outside `/healthz`, `/readyz`, and CORS preflight. |

Service name, API version, service version, public base URL, and minimum object
timeouts are owned by `src/app/tamoss/settings.py` and persisted service metadata.

## Authentication

| Variable | Default | Description |
|----------|---------|-------------|
| `TAMOSS_API_TOKEN` | _(unset)_ | Static token accepted as `Authorization: Bearer`. The Helm chart writes this key to the API token Secret. |
| `TAMOSS_BASIC_AUTH_USERNAME` | `tamoss` | Username accepted by HTTP Basic auth. |
| `TAMOSS_BASIC_AUTH_PASSWORD` | API token | Password accepted by HTTP Basic auth. |
| `TAMOSS_TRUST_FORWARD_AUTH_HEADERS` | `false` | Trust ingress-injected `Remote-User` or `X-Authentik-Username` headers. Only enable when direct API access is blocked and caller-supplied identity headers are stripped. |
| `TAMOSS_OAUTH2_ENABLED` | `false` | Validate OAuth2/OIDC JWT bearer tokens in addition to the static bearer token. |
| `TAMOSS_OAUTH2_ISSUER` | _(unset)_ | Expected JWT issuer. |
| `TAMOSS_OAUTH2_JWKS_URI` | _(unset)_ | JWKS endpoint used to verify JWT bearer-token signatures. Required when OAuth2 is enabled. |
| `TAMOSS_OAUTH2_AUDIENCE` | _(unset)_ | Optional expected JWT audience. |
| `TAMOSS_OAUTH2_REQUIRED_SCOPES` | _(unset)_ | Optional comma-separated scopes required in the JWT `scope` or `scp` claim. Empty means issuer/signature validation is enough. |
| `TAMOSS_OAUTH2_ALGORITHMS` | `RS256` | Comma-separated JWT signing algorithms accepted from the issuer. |
| `TAMOSS_OAUTH2_JWKS_CACHE_SECONDS` | `300` | JWKS cache lifetime for OAuth2 bearer-token validation. |
| `TAMOSS_OAUTH2_JWKS_TIMEOUT_SECONDS` | `5` | Network timeout for JWKS fetches. |

## Concurrency Sizing

FastAPI runs normal `def` route handlers in the Starlette/anyio threadpool. For
the synchronous PostgreSQL and S3 adapters, size the database and S3 pools with
that threadpool rather than independently:

```text
pod_count * threadpool_size <= postgres_max_connections - headroom
TAMOSS_DATABASE_POOL_MAX_SIZE ~= threadpool_size
TAMOSS_S3_MAX_POOL_CONNECTIONS >= threadpool_size
```

The Starlette/anyio default threadpool size is 40. If the API process changes
that value, update the database pool and S3 client pool values in the same
deployment change.

## Webhooks And Workers

| Variable | Default | Description |
|----------|---------|-------------|
| `TAMOSS_WEBHOOK_TIMEOUT_SECONDS` | `30` | Outbound webhook POST timeout. |
| `TAMOSS_WORKER_MAX_ATTEMPTS` | `5` | Maximum webhook delivery attempts before terminal failure. |
| `TAMOSS_WORKER_POLL_INTERVAL_SECONDS` | `5` | Seconds between worker poll cycles. |
| `TAMOSS_WORKER_MAX_REQUESTS` | `50` | Maximum delete requests or webhook deliveries claimed per worker cycle. |
| `TAMOSS_WORKER_LEASE_SECONDS` | `300` | Seconds before a claimed delete request or webhook delivery can be reclaimed by another worker. |
| `TAMOSS_WORKER_ID` | `<hostname>:<pid>` | Optional stable worker identifier stored in queue claim metadata. |
| `TAMOSS_WORKER_ENABLE_DELETE` | `1` | Enable delete-request processing. |
| `TAMOSS_WORKER_ENABLE_WEBHOOK` | `1` | Enable webhook delivery processing. |
| `TAMOSS_LOG_LEVEL` | `LOG_LEVEL` or `INFO` | Worker log level. |

## Deployment Profiles

The Helm umbrella chart is `deploy/charts/tams-stack`. It renders the TAMOSS API,
worker, UI, PostgreSQL, RustFS, Gateway API resources, and optional authentik
integration depending on the selected values file.

| Profile | Values file | Included surface |
|---------|-------------|------------------|
| Lite | `deploy/charts/tams-stack/values-lite.yaml` | API, PostgreSQL, RustFS/S3-compatible storage, generated API token. |
| Full | `deploy/charts/tams-stack/values-full.yaml` | Lite surface plus UI, background workers, Gateway API routes, ReferenceGrant, and forward-auth integration points. |

The chart and default namespace still use `tams` because the public API and host
names are BBC TAMS-facing. Runtime modules, Python package metadata, and new
configuration variables use `tamoss` / `TAMOSS`.

## Configuration Precedence

Database URL resolution is:

1. `TAMOSS_DATABASE_URL`
2. `DATABASE_URL`
3. `postgresql://...` built from `POSTGRES_*` variables when `POSTGRES_HOST` is set

Storage backend resolution uses the default S3-compatible backend when both
`TAMOSS_S3_ACCESS_KEY` and `TAMOSS_S3_SECRET_KEY` are set.

Media ingest needs a backend that can issue client-facing HTTP object URLs. Production
and Kind installs use S3-compatible storage with presigned PUT/GET URLs.

## See Also

- `CONTRIBUTING.md` - development setup and test recipes
- `deploy/charts/tams/values.yaml` - application Helm chart configuration
- `deploy/charts/tams-stack/values.yaml` - stack-level dependency configuration
- `deploy/compose/docker-compose.yaml` - local dev dependency stack
- `src/app/tamoss/settings.py` - runtime configuration
- `src/app/tamoss/adapters/object_storage.py` - object-storage adapter
