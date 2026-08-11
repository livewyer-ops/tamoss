# Frontend architecture

## Boundaries

- The generated TAMS client owns media-store protocol access.
- The same-origin Console API owns namespace-scoped Kubernetes runtime access.
  Its current read-only scaffold is explicit opt-in for trusted development
  environments until end-user authentication, roles, owner-chain validation,
  diagnostic redaction, and audit are implemented.
- The browser never receives Kubernetes credentials or submits arbitrary Job specifications.
- The UI's shared-token `/api/` proxy permits only `GET`, `HEAD`, and `OPTIONS`. TAMS mutations require a future end-user-scoped path; typed operational commands use the Console API.
- `player/MediaPreview.tsx` is the lazy integration boundary for Omakase. Keep it dependency-free until a security-reviewed Omakase core and TAMS adapter combination is available.
- Runtime reports Tamsin Jobs as ephemeral execution telemetry. Durable run history and create, cancel, and retry controls are gated on a versioned `IngestRun` command and event contract.

## Catalogs

Sources and Flows keep one response page in component state and at most four previous cursors. The supported TAMS 8.1 exact filters are reflected in the URL. The UI must not download every page to implement search, sorting, or totals.

Full-text search, arbitrary sorting, totals, reverse traversal, saved views, and query-wide bulk actions require a versioned server API. Do not simulate them over a partial client-side result set.

## Runtime stream

The UI first requests `/ui-api/v1/runtime`. It opens `/ui-api/v1/runtime/events` only after that request returns a snapshot, preventing reconnect loops on installations without the Console API. SSE carries latest-state snapshots; polling remains the recovery path.
