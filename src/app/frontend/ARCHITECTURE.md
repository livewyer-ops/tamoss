# Frontend architecture

## Boundaries

- The generated TAMS client owns media-store protocol access.
- The same-origin Console API owns namespace-scoped Kubernetes runtime access.
  Its current read-only runtime surface authenticates requests forwarded by
  the UI proxy and requires a viewer-capable operator-managed role.
- The browser never receives Kubernetes credentials or submits arbitrary Job specifications.
- The browser never receives an API or forwarding credential. In production,
  nginx authenticates `/api/` and `/ui-api/` with an internal Authentik
  `auth_request`, discards all browser request headers, and forwards only an
  allow-listed protocol and identity set with its mounted proof.
- The `/api/` proxy permits only `GET`, `HEAD`, and `OPTIONS`. TAMS mutations
  require a future end-user-scoped path; typed operational commands will use
  the Console API after its command contract and audit trail are implemented.
- `player/MediaPreview.tsx` is the lazy integration boundary for Omakase. Keep it dependency-free until a security-reviewed Omakase core and TAMS adapter combination is available.
- Runtime reports Tamsin Jobs as ephemeral execution telemetry. Durable run history and create, cancel, and retry controls are gated on a versioned `IngestRun` command and event contract.

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

## Catalogs

Sources and Flows keep one response page in component state and at most four previous cursors. The supported TAMS 8.1 exact filters are reflected in the URL. The UI must not download every page to implement search, sorting, or totals.

Full-text search, arbitrary sorting, totals, reverse traversal, saved views, and query-wide bulk actions require a versioned server API. Do not simulate them over a partial client-side result set.

## Runtime stream

The UI first requests `/ui-api/v1/runtime`. It opens `/ui-api/v1/runtime/events` only after that request returns a snapshot, preventing reconnect loops on installations without the Console API. SSE carries latest-state snapshots; polling remains the recovery path.
