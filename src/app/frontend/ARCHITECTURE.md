# Frontend architecture

## Boundaries

- The generated TAMS client owns media-store protocol access.
- The same-origin Console API owns namespace-scoped Kubernetes runtime access.
  Runtime and durable `IngestRun` reads require a viewer-capable session;
  one-way cancellation additionally requires the operator role, an exact UID
  and revision, and same-origin validation.
- The browser never receives Kubernetes credentials or submits arbitrary Job specifications.
- The browser never receives an API or forwarding credential. In production,
  nginx authenticates `/api/` and `/ui-api/` with an internal Authentik
  `auth_request`, discards all browser request headers, and forwards only an
  allow-listed protocol and identity set with its mounted proof.
- The `/api/` proxy permits only `GET`, `HEAD`, and `OPTIONS`. TAMS mutations
  require a future end-user-scoped path; typed operational commands use the
  Console API and must declare session capabilities and produce audit records.
- `player/MediaPreview.tsx` is the lazy integration boundary for the exact-pinned
  Omakase core. Its descriptor, URL policy, manifest compiler, DOM fallback,
  dependency budget, and cleanup rules stay independent of catalog routes.
- Runtime reports Tamsin Jobs as ephemeral execution telemetry. `/ingest` uses
  cursor-paginated `IngestRun` resources for durable history and exposes only
  the currently supported cancel command. Create and retry stay absent until
  the Tamsin image, resolvers, collector, and artifact contract are complete.

## Proxy authentication

Production containers require `TAMOSS_UI_AUTH_MODE=authentik`,
`TAMOSS_AUTHENTIK_AUTH_REQUEST_URL`, and an API proof of at least 32 characters
at `TAMOSS_API_FORWARD_AUTH_SHARED_SECRET_FILE` (default
`/run/tamoss/forward-auth/api-proof`). When the Console upstream is enabled, a
distinct proof is required at `TAMOSS_CONSOLE_FORWARD_AUTH_SHARED_SECRET_FILE`
(default `/run/tamoss/forward-auth/console-proof`). The UI receives both issuer
proofs; API and Console each receive only their matching verifier proof. Updating
either proof requires a UI pod restart because nginx reads it during startup.

`TAMOSS_UI_AUTH_MODE=none` is an explicit local-development mode. Vite can add a
development-only upstream Bearer credential from `TAMOSS_DEV_API_TOKEN`; it is
read by the Vite process and is never exposed through `import.meta.env`.
Required authentication without the managed Authentik proxy renders
`TAMOSS_UI_AUTH_MODE=unavailable`; nginx returns `503` for both browser backend
paths and never opens a credential-free proxy. External OIDC needs a
same-origin browser session before that mode can be enabled; the proposed
boundary is recorded in
[`0006-external-browser-identity.md`](../../../docs/development/ui-overhaul/0006-external-browser-identity.md).

## Browser response boundary

`X-Forwarded-Proto` is client-supplied, so the externally visible scheme is
honoured only for private-network peers and otherwise falls back to the scheme
nginx served. The Authentik subrequest and both browser backend paths use that
one normalised value. A deployment that publishes the container port beyond the
cluster network must narrow the trusted set in `nginx.conf`.

Every response carries the same `Content-Security-Policy`. Scripts, styles and
documents stay same-origin; script execution permits the narrow
`'wasm-unsafe-eval'` source needed by Omakase's waveform engine without
permitting JavaScript `eval()`. Scripts and workers allow `blob:` for the
generated AudioWorklet and media worker, while media and XHR allow it for
generated manifests. Media and XHR allow `data:` for Omakase's bundled silent
audio and WASM assets, and `https:` because presigned object storage URLs are
only known at deployment time. A location that defines its own `add_header`
stops inheriting the server-level headers, so every such location repeats the
full set.

## Catalogs

Sources and Flows keep one response page in component state and a bounded trail of previous cursors, deep enough to reverse a whole hand-driven traversal. The supported TAMS 8.1 exact filters are reflected in the URL. The UI must not download every page to implement search, sorting, or totals.

Full-text search, arbitrary sorting, totals, reverse traversal, saved views, and query-wide bulk actions require a versioned server API. Do not simulate them over a partial client-side result set.

## Runtime stream

The UI first requests `/ui-api/v1/runtime`. It opens `/ui-api/v1/runtime/events` only after that request returns a snapshot, preventing reconnect loops on installations without the Console API. SSE carries latest-state snapshots; polling remains the recovery path.
