# Frontend API Strategy

TAMOSS generates frontend API schema types from `src/openapi.yaml`, the checked
public OpenAPI contract that is validated against the runtime backend by
`task openapi:parity`. Local TAMOSS type aliases stay in `src/types/tams.ts`
rather than changing generated output.

Update generated contract types with:

```bash
npm --prefix src/app/frontend run api:types
```

Generated output lives in `src/api/generated/openapi.ts`. It is checked into the
repo for deterministic frontend type checking and excluded from ordinary
format/lint churn.

All page code should call `TamossApiClient`, which inherits the shared
`ApiTransport` request path. Endpoint request and response aliases in
`src/types/tams.ts` are derived from the generated contract and keep local
typing choices close to the API boundary.

Data loading should use `useApiQuery` or a focused workflow hook. The hook is
backed by TanStack Query with the shared `apiQueryPolicy` in `src/api/query.ts`;
mutations should invalidate `apiQueryKeys.all` or a narrower key from that
module rather than scattering cache-key strings in page components.
