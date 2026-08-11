# 0002: Console API, Kubernetes Identity, and RBAC

## Status

- The backend boundary and security model are accepted for TAMOSS 8.2.
- An informer-backed Console API exists and remains explicit opt-in.
  Managed Authentik sessions now cross a proof-backed same-origin proxy and
  exact group bindings authorize the `viewer`, `operator`, and `ingest-runner`
  roles. The API exposes bounded runtime telemetry, cursor-paginated durable
  `IngestRun` reads, session capabilities, and audited one-way cancellation.
  Its checked OpenAPI contract generates the frontend operation types and is
  verified against the Go response structs. Create and retry remain unavailable.
- Runtime projection now validates exact UID-bound controller chains for
  Kubernetes children and exact typed references for `StorageBackend` and
  `IngestRun` roots. The namespace remains the tenancy boundary; owner
  references are an output-integrity check, not an authorisation boundary.
- Free-form Kubernetes condition, Pod, Job, and Event messages are not exposed.
  Diagnostic types and reasons are constrained to bounded machine-code syntax;
  malformed values project as `Unknown`.
- Constrained Kubernetes API egress, external OIDC, automated deployed role
  tests, and scale evidence remain gates before read access is enabled by
  default, not only before mutation controls are enabled.

## Context

The UI must explain instance, workload, Pod, Job, and Event state and must
start and control Tamsin ingest. The BBC TAMS API is deliberately
Kubernetes-agnostic, while a browser-held Kubernetes token would grant a much
larger and less auditable authority than the product needs.

Kubernetes RBAC cannot restrict `list` and `watch` by an instance label. The
security boundary is therefore the namespace, consistent with
[TAMOSS tenancy](../../concepts/tenancy.md). Put instances in separate
namespaces when their users must not observe one another's workload metadata.

## Accepted Decisions

The operator deploys a dedicated Console API for each enabled TAMOSS instance.
The UI reverse proxy exposes it on the same origin under `/ui-api/v1/`; the
standards-compatible TAMS API remains under `/api/`.

The browser never receives a service-account token and never calls the
Kubernetes API. The Console API uses namespaced informers, bounded direct reads
for referenced durable roots, and typed command handlers. It returns product
read models, not unrestricted Kubernetes objects.

The [Console OpenAPI document](../../../operator/internal/consoleapi/openapi.yaml)
is the browser contract. Generated operation types feed one reviewed frontend
adapter; reproducibility and Go JSON-field parity are test gates.

The minimum read surface is:

| Read model | Sources |
| --- | --- |
| Instance | `Tamoss` conditions, resolved release, and selected providers |
| Services | operator-owned Deployments, Services, EndpointSlices, and Pods |
| Operations | operator-owned Jobs and `IngestRun` summaries |
| Diagnostics | bounded, sanitised Kubernetes Events for owned resources |

Status starts with the `Tamoss` and `StorageBackend` conditions. Pod and Event
details are a diagnostic drill-down, not the primary product health model.
TAMOSS labels bound discovery for Kubernetes workload resources. Exact
API-version, kind, namespace, name, UID, and controller references then bind
Deployments to Tamoss, ReplicaSets to Deployments, Pods to retained
ReplicaSets or Jobs, and EndpointSlices to Services. `StorageBackend` and
`IngestRun` are typed logical roots selected by their exact
`spec.tamossRef.name`; Jobs bind to those roots by UID. The API fails closed
when a chain cannot be established. These checks prevent accidental
cross-instance projection but do not protect against a principal that can
already write arbitrary resources in the namespace.

List/watch data is cached server-side and projected to clients. Durable
`IngestRun` history is read separately with opaque, query-bound Kubernetes
continuation cursors, a maximum of 100 results and 32 backend pages per request,
four-second deadlines, and bounded concurrent reads. Sparse instance or phase
filters can return an empty page with a next cursor; clients must not treat that
as the end of history or calculate totals. `IngestRun` history is never held in
an informer cache.

The runtime view does not scan durable history: at most 16 active, nonterminal,
then newest roots referenced by discovered Jobs are
verified with exact live GETs under one four-second deadline. Successful
immutable identity checks are cached for 30 seconds. When that budget is
exceeded, the runtime response and UI identify the ingest Job, Pod, and Event
projection as partial.

Server-sent events provide coalesced complete snapshots. Event IDs are
diagnostic; reconnecting receives the latest snapshot and 8.2 provides no
replay history. The API must bound per-client queues and disconnect slow
clients rather than allowing a Kubernetes watch to consume unbounded memory.

## Command Boundary

The Console API exposes typed product commands only. The accepted 8.2 surface is:

- create an approved `IngestRun`;
- request cancellation of an active `IngestRun`; and
- create a retry run from a terminal `IngestRun`.

It does not expose Secret values, arbitrary Kubernetes YAML, container
commands, exec, attach, port-forward, raw log queries, or generic create,
patch, and delete endpoints. It cannot create or modify Jobs; the operator
does that after validating an `IngestRun`.

The implemented command surface is deliberately narrower: cancellation accepts
only an exact run name, UID, and resource revision, verifies same-origin browser
provenance, and optimistically patches `spec.desiredState` from `Running` to
`Cancelled`. Replays are idempotent. Create and retry remain unavailable and are
advertised as such by `/ui-api/v1/session` until their resolver and artifact
contracts are complete.

## Identity and Authorisation

The Console API authenticates the same user session as the UI. The implemented
production path is trusted forward-auth; direct validated OIDC bearer support
remains a release gate.
Forwarded identity headers are accepted only with an operator-generated proof
header. API and Console use distinct proofs, and each verifier receives only its
own key; the reverse proxy strips any browser-supplied proof or identity header
before adding trusted values.

Claims map to additive application roles:

| Role | Console authority |
| --- | --- |
| `viewer` | Read instance, catalog, workload, run, and sanitised Event state |
| `operator` | `viewer`, cancel active runs, and retry terminal runs |
| `ingest-runner` | `viewer` and create runs using approved profiles and credentials |

No role implies arbitrary Kubernetes administration. TAMS metadata mutations
continue to require their TAMS API scopes. A shared high-privilege TAMS token
must not silently give every authenticated UI user write or delete access.

Every command emits an audit record containing the authenticated subject,
roles, request ID, instance, command, target, outcome, and reason code. It must
not contain bearer tokens, Secret data, presigned URL queries, media locators,
or request bodies that can contain credentials.

## Kubernetes RBAC

The Console API service account receives one generated namespace `Role`:

- exact-instance `get`, `list`, and `watch` for `tamosses`;
- `get`, `list`, and `watch` for `storagebackends`, Deployments, ReplicaSets,
  Services, EndpointSlices, Pods, Jobs, and Events; and
- `get` and `list` for durable `ingestruns`, plus `patch` for the typed one-way
  cancellation handler. Kubernetes RBAC cannot restrict these dynamic names
  with `resourceNames`.

Future create and retry handlers must not broaden this to generic update,
delete, status, Job, or Secret authority.

It receives no Secret, ConfigMap, Pod log, Pod subresource, Job write, or
cluster-scoped permission. Resource-name restrictions are used for `get` where
possible, but they do not replace the namespace boundary for list/watch.

The operator retains Job lifecycle authority. Console and operator service
accounts are distinct, use projected short-lived tokens, disable token
automount on unrelated containers, and are subject to NetworkPolicy. The
initial multi-server policy permits outbound HTTPS generally because it cannot
derive an API-server destination from the CR; that is not an API-only egress
boundary.

## Failure Behaviour

- An unavailable Kubernetes watch or referenced-root read makes operational
  data stale; it does not make cached data appear current.
- Authorisation failures are `403` with a stable reason code. Missing identity
  is `401`; neither response reveals the required group name.
- Unknown or no-longer-owned resources are absent, not returned as raw data.
- Console failure does not make the TAMS API unavailable.

## 8.2 Release Gates

Synthetic scale regressions now traverse a sparse 10,000-run durable history
through bounded continuation pages and resolve a 10,000-active-Job runtime set
with exactly 16 live `IngestRun` reads. The runtime test verifies newest active
roots win, `ingestRuntimeTruncated` is set, and the 30-second positive cache
eliminates repeat reads. This is bounded-algorithm evidence, not a substitute
for API-server, informer fan-out, slow-client, or concurrent-ingest load tests.

- Complete typed create and retry commands without broadening the implemented
  cursor-paginated read and cancellation authority. Do not present ephemeral
  Jobs as durable run history.
- Keep the shared UI-to-TAMS API token removed and prove every future TAMS
  mutation is authorised using the authenticated human subject and scopes,
  including direct requests outside rendered controls. Until then `/api/` is
  strictly limited to `GET`, `HEAD`, and `OPTIONS` by the UI proxy and the API
  independently limits trusted forward-auth identities to `GET` and `HEAD`.
- Prove header spoofing, expired JWT, wrong audience, cross-namespace access,
  and privilege escalation attempts fail.
- Load-test the bounded referenced-root resolver and partial-result signal with
  large durable run histories and high concurrent ingest rates.
- Restrict Console Kubernetes API egress using configured API-server CIDRs or
  a constrained in-cluster proxy, and test that arbitrary HTTPS exfiltration is
  denied. A port-only `443` rule is insufficient.
- Assert the rendered Role contains no wildcard, Secret, exec, log, or Job
  write permission.
- Build, sign, and publish the Console image; validate generated CRDs and the
  install bundle, UI upstream wiring, and a deployed same-origin smoke test.
- Keep each generated CRD below the enforced 1,350,000-byte budget and avoid
  further duplicated inline workload schemas; the Tamoss CRD is already close
  to the Kubernetes API object-size ceiling.
- Verify all commands produce redacted, attributable audit records.
- Load-test informer fan-out, reconnect, and slow-client handling without one
  watch per browser.
- Validate `viewer`, `operator`, and `ingest-runner` with managed Authentik and
  external OIDC configurations.

## References

- [Kubernetes API list/watch semantics](https://kubernetes.io/docs/reference/using-api/api-concepts/)
- [Kubernetes RBAC good practices](https://kubernetes.io/docs/concepts/security/rbac-good-practices/)
