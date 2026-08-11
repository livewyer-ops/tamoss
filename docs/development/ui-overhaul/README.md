# UI Overhaul Design Records

These records define the TAMOSS 8.2 UI architecture and the evidence required
to release it. They are product contracts; each status section separately
states how much of that contract is implemented.

Each record separates:

- **Accepted decisions**, which implementations must follow; and
- **8.2 release gates**, which still need evidence before the new UI becomes
  the default.

| Record | Accepted boundary | Principal release gate |
| --- | --- | --- |
| [0001: Frontend and dependencies](0001-frontend-and-dependencies.md) | React, TypeScript, Vite, typed API adapters, a small component layer, and measured dependency budgets | Prove deployed journeys and the dependency budgets in CI |
| [0002: Console API and Kubernetes](0002-console-api-and-kubernetes.md) | Same-origin, namespace-scoped backend; no Kubernetes credentials or arbitrary workloads in the browser | Prove identity, authorisation, audit, and least-privilege RBAC end to end |
| [0003: Large catalog queries](0003-large-catalog-queries.md) | Bounded server-side query and keyset pagination contract | Meet the query budget with 10 million synthetic records |
| [0004: Omakase preview](0004-omakase-preview.md) | Omakase behind a lazy, read-only preview adapter | Resolve package compatibility and pass media, security, and accessibility tests |
| [0005: Tamsin ingest runs](0005-tamsin-ingest-runs.md) | Declarative `IngestRun`, operator-created Jobs, and pinned versioned Tamsin events | Promote a matching Tamsin image/decoder and pass interruption, scale, and credential-boundary tests |

The records may be amended before 8.2 while a release gate is unresolved. A
change to an accepted boundary must update the relevant record and explain the
trade-off; implementation drift alone is not a new decision.
