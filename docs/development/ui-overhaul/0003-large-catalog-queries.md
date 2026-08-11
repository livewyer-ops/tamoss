# 0003: Large Catalog Query and Pagination Contract

## Status

- The browser and API contract is accepted for TAMOSS 8.2.
- The current UI implements bounded TAMS 8.1 exact-filter pagination only.
  It cancels superseded browser requests and keeps a four-cursor back window.
  Catalog search, server keyset cursors, sorting, Objects listing, and the
  10-million-record benchmark are not implemented.
- The PostgreSQL-only implementation versus a separate search index remains a
  release-gate outcome, not an assumption.

## Context

TAMOSS implements the [BBC TAMS 8.1 contract](../../../src/openapi.yaml).
TAMS list endpoints provide opaque forward cursors and exact filters, but do
not define totals, arbitrary sorting, full-text search, reverse navigation, or
a list-all-Objects endpoint. The current UI cannot correctly add those features
by loading every page: the result is incomplete until the whole store is in
memory and does not scale to a large store.

## Accepted Decisions

The browser uses a TAMOSS-specific catalog contract under
`/ui-api/v1/catalog/{sources|flows|objects}`. This does not alter the semantics
of the standards-compatible `/api/` TAMS endpoints.

Each request is one bounded server-side query. Neither the browser nor the
Console API may crawl and concatenate TAMS pages to answer search, sort,
facets, or totals. The catalog implementation queries an authoritative indexed
store through a dedicated repository boundary.

### Request

The common request fields are:

| Field | Contract |
| --- | --- |
| `cursor` | Opaque server-issued cursor; omitted for the first page |
| `limit` | Default `50`; accepted values `25`, `50`, `100`, and `200` |
| `q` | At most 256 Unicode characters after NFKC normalisation, trimming, whitespace collapse, and case folding |
| `sort` | Resource-specific allow-list; defaults to most recently updated |
| `direction` | `asc` or `desc` |
| exact filters | Repeated typed query fields, never an executable expression |

Sources support exact `id`, `format`, `label`, and tag filters. Flows support
exact `id`, `source_id`, `format`, `codec`, `label`, timerange, dimensions, and
tag filters. Objects support exact or prefix `id`, referencing `flow_id`, and
storage filters. Detail views continue to use the TAMS resource endpoints.

`q` searches documented summary fields only: identifiers, label, and
description for Sources and Flows; identifier for Objects. It is not a promise
to search arbitrary JSON or media content. An identifier-shaped query first
offers exact lookup without waiting for a text search.

Allowed sort keys are:

- Sources: `updated`, `created`, `label`, and `id`;
- Flows: `metadata_updated`, `segments_updated`, `created`, `label`, and `id`;
  and
- Objects: `created` and `id`.

All sorts add `id` as a deterministic tie-breaker and define nulls last.

### Response

```json
{
  "items": [],
  "page": {
    "limit": 50,
    "next_cursor": null,
    "query_id": "q_01J..."
  }
}
```

List items are compact, resource-specific summaries. Full tags, Flow
collections, Segment lists, storage URLs, and other unbounded relationships
are fetched only by detail views.

`query_id` is an opaque, keyed identifier for diagnostics; it does not expose a
plain hash of possibly sensitive query text. `next_cursor` is absent or `null`
at the end. A response does not imply a total count. Facets and counts, if
added, use separate explicitly requested endpoints with their own latency and
freshness contract.

### Cursor Semantics

Cursors use keyset pagination over the effective sort tuple. They are opaque,
tamper-evident, versioned, and bound to the resource, normalised query,
filters, sort, direction, and limit. Reusing a cursor with changed query state
returns `400 invalid_cursor`; expired cursors return `410 cursor_expired`.

Do not use offset as the cursor boundary. Concurrent changes may alter later
pages, so responses include a `query_id` for diagnostics and the UI labels a
manual refresh as a new result set. The UI retains only three to five visited
pages and their cursors. Query, filters, sort, limit, and the active cursor live
in the URL so browser Back and shared links are meaningful.

Changing any query field resets to the first page. Requests are debounced and
cancelled when superseded. Auto-refresh does not move a user through catalog
pages; it reports that newer results are available.

## UI Behaviour

- Render a semantic table with stable column widths and a page-scoped
  selection model.
- Persist column visibility, order, density, and page size separately from the
  shareable query URL.
- Never label client filtering of a loaded page as catalog search.
- Never fabricate a total. Use `More results` while a next cursor exists.
- Bulk actions apply to selected rows on the current page in 8.2. A future
  query-wide action must be a server-side operation with an explicit preview
  and confirmation.
- Virtualise a bounded page only after measurement; virtualisation is not a
  replacement for pagination.

## Index and Query Rules

Every supported filter and sort combination needs an intentional index or a
documented fallback. User text is always parameterised. Query execution has a
database timeout and cancellation propagates when the browser request is
aborted.

The service records duration, resource, result count, filter names, sort, and a
query fingerprint. It does not log query text, tag values, cursor contents, or
media identifiers.

## 8.2 Release Gates

- Seed at least 10 million representative Sources and Flows plus associated
  Objects, tags, and relationships.
- On documented 2-vCPU/8-GiB test infrastructure, demonstrate p95 below one
  second for first pages and below 500 ms for subsequent indexed pages under
  ten concurrent browsing sessions.
- Verify supported plans do not contain an offset walk or unbounded in-memory
  sort, and responses remain below 1 MiB at `limit=200`.
- Test insertion, update, and deletion between pages; cursor tampering,
  expiry, and query mismatch; cancellation; and pathological text/tag input.
- Choose PostgreSQL indexes or a separate search service from the benchmark
  evidence and record its operational ownership before implementation merges.

## References

- [Cloudscape table-view pattern](https://cloudscape.design/patterns/resource-management/view/table-view/)
- [WAI-ARIA table pattern](https://www.w3.org/WAI/ARIA/apg/patterns/table/)
