# 0002: Console API, Kubernetes Identity, and RBAC

## Status

- The backend boundary and security model are accepted for TAMOSS 8.2.
- An informer-backed Console API exists and remains explicit opt-in.
  Managed Authentik sessions now cross a proof-backed same-origin proxy and
  exact group bindings authorise the `viewer`, `operator`, and `ingest-runner`
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
- Multi-server NetworkPolicy and bounded 10,000-item scale regressions are
  implemented. Destination-scoped egress is deferred: default egress is
  port-scoped, as it was in 8.1, because the destination-scoped rules could not
  be verified against an enforcing CNI in CI. External browser OIDC remains a
  gate before read access is enabled by default, not only before mutation
  controls are enabled.

## Context

The UI must explain instance, workload, Pod, Job, and Event state and must
start and control [TAMSin](https://github.com/livewyer-ops/tamsin) ingest. The
BBC TAMS API is deliberately Kubernetes-agnostic, while a browser-held
Kubernetes token would grant a much larger and less auditable authority than
the product needs.

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
advertised as such by `/ui-api/v1/session` until their resolver and artefact
contracts are complete.

## Identity and Authorisation

The Console API authenticates the same user session as the UI. The implemented
production path is trusted forward-auth; direct validated OIDC bearer support
remains a release gate.
Forwarded identity headers are accepted only with an operator-generated proof
header. API and Console use distinct proofs, and each verifier receives only its
own key; the reverse proxy strips any browser-supplied proof or identity header
before adding trusted values.

Claims map to additive application roles. A group binding accepts exactly four
permission values; any other value is rejected when the binding is validated.

| Permission | Console authority |
| --- | --- |
| `viewer` | Read instance, catalog, workload, run, and sanitised Event state |
| `operator` | `viewer`, cancel active runs, and retry terminal runs |
| `ingest-runner` | `viewer` and create runs using approved profiles and credentials |
| `admin` | Compatibility alias for the three roles above; no extra authority |

`operator` and `ingest-runner` each imply `viewer`, and `admin` expands to all
three before the identity is built, so `/ui-api/v1/session` only ever reports
`viewer`, `operator`, and `ingest-runner`. The TAMS API treats the four
permissions identically: each grants the read scope alone, and forward-auth
identities remain limited to `GET` and `HEAD` on explicitly mapped read routes
whatever their role.

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
automount on unrelated containers, and are subject to NetworkPolicy.
Destination-scoped egress is deferred for 8.2. Default egress is port-scoped
only, as it was in 8.1: multi-server UI egress allows the DNS ports plus the
API, Console, and Authentik forward-auth ports, and Console API egress allows
the DNS ports plus Service port 443 and the common post-DNAT target port 6443.
Any destination on those ports is permitted, so this is a weaker boundary than
naming destinations; it is not equivalent.

`spec.networkPolicy.kubernetesAPIIPBlocks` remains supported as an optional
tightening. Supplying the Kubernetes Service and API-server endpoint CIDRs
scopes the Console 443 and 6443 rule to those destinations. It is not required,
and enabling Console without it is accepted on every profile.

Destination scoping was deferred rather than shipped because it could not be
verified against an enforcing CNI in CI, and an unverified peer list is a worse
outcome than a port-scoped rule. Naming DNS resolvers in particular must cover
every resolver a supported cluster may run, since an unmatched resolver removes
DNS from the workload entirely and presents as a total outage rather than a
policy error. A cluster that wants destination-scoped egress today can declare
explicit rules under `spec.networkPolicy.<component>.egress`, which the operator
renders unchanged.

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

There is no deployed egress deny regression in 8.2. The destination-scoped rules
it covered are deferred, and the check could not be run on an enforcing CNI in
CI, so the rules and the check were removed together rather than left as
unverified assertions. Rendered policy shape is covered by operator unit tests
only, which prove what is declared, not what a CNI enforces.

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
- Restore destination-scoped UI and Console egress with a deployed deny
  regression that runs on the release GCP CNI as well as a recorded Cilium
  configuration. Cilium needs `policyCIDRMatchMode: nodes` for its standard
  `NetworkPolicy.ipBlock` implementation to match a self-hosted API server, so
  the peer list must be proven per CNI rather than assumed. When external OIDC
  is enabled, include its exact provider or egress-proxy destinations without
  permitting arbitrary DNS or HTTPS traffic.
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
