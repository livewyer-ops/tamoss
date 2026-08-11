# Frontend API Strategy

TAMOSS generates frontend schema types from two checked contracts:

- `src/openapi.yaml` is the public TAMS API contract validated by
  `task openapi:parity`.
- `operator/internal/consoleapi/openapi.yaml` is the browser Console API
  contract implemented by the operator-owned Console service.

Local TAMS type aliases stay in `src/types/tams.ts`, and Console aliases stay
next to their adapters under `src/control`, rather than changing generated
output.

Update generated contract types with:

```bash
npm --prefix src/app/frontend run api:types
```

Generated output lives in `src/api/generated/openapi.ts` and
`src/control/generated/openapi.ts`. Both are checked into the repo for
deterministic frontend type checking and excluded from ordinary format/lint
churn. `npm run api:types:check` fails when either output is stale.

All page code should call `TamossApiClient`, which inherits the shared
`ApiTransport` request path. Endpoint request and response aliases in
`src/types/tams.ts` are derived from the generated contract and keep local
typing choices close to the API boundary.

Data loading should use `useApiQuery` or a focused workflow hook. The hook is
backed by TanStack Query with the shared `apiQueryPolicy` in `src/api/query.ts`;
mutations should invalidate `apiQueryKeys.all` or a narrower key from that
module rather than scattering cache-key strings in page components.
