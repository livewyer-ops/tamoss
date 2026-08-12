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

Data loading goes through TanStack Query directly — there is no wrapper hook.
Pages call `useQuery` with the client from `useApi()`
(`src/contexts/ApiContext.tsx`). Shared defaults for retry, `staleTime` and
`refetchOnWindowFocus` are set once on the `apiQueryClient` in
`src/api/query.ts` and apply to every query, so page code supplies only
`queryKey` and `queryFn`.

Query keys are arrays scoped by their first element: `["api", ...]` for the TAMS
API and `["control", ...]` for the Console API. Console keys are named constants
in the `src/control/` hooks because mutations invalidate them; TAMS keys are
written at the call site. A mutation that changes a resource should invalidate
the narrowest key covering it.

Catalog routes page through `useCursorPage` (`src/hooks/useCursorPage.ts`),
which holds cursor state and request cancellation itself rather than through
TanStack Query.
