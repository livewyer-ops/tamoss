# Known Issues

Accepted deviations from the [Cloud Native Operational Engineering Standards](CLAUDE.md#engineering-standards-canoes)
and defects we have chosen not to fix yet.

Record an entry when a change knowingly breaks a standard, or when a defect is
understood but deferred. Each entry states the deviation, why it was accepted,
who it affects, and what resolving it would take. Remove the entry when the
underlying issue is fixed.

Entries were last re-verified against the 8.2 console refresh (`5ec9df1`).

## Service name is not reproducible from the codebase

**Standard:** Single source of truth; everything reproducible from the codebase.

The service display name has two sources. `.spec.displayName` on the `Tamoss` CR
renders `TAMOSS_SERVICE_NAME` onto the API deployment and acts as a startup
default. `POST /service` (`api/routes/service.py:45`) writes a row to
`tamoss_service_metadata`, and that stored row takes precedence for the life of
the instance (`adapters/postgres_repository/storage_service.py:31-64`).

A stored name is therefore not reconciled back by the operator, and the live
name cannot be rebuilt from the repository. Two operators can disagree: one sets
`displayName` in Git, another posts a rename, and the stored row wins silently.

**Why accepted:** the TAMS API exposes `POST /service` as part of the contract,
so the write path cannot simply be removed. Making the CR authoritative would
mean refusing a call the specification requires us to accept.

**Changed by the 8.2 console refresh:** the UI no longer offers a rename. The
inline edit is gone, the `updateServiceInfo` client method has been deleted, and
in production the `/api/` proxy permits only `GET`, `HEAD` and `OPTIONS`
(`src/app/frontend/ARCHITECTURE.md`), so the console cannot reach the write path
at all. The row is now settable only by a direct API client — and still not
clearable: an empty save writes `NULL`, and a `NULL` row still shadows the
configured value, so an instance cannot be returned to its CR-declared name
without a direct `DELETE FROM tamoss_service_metadata`.

**Resolving it would take:** treating an empty write as a delete of the row, so
the stored name becomes a true override-with-reset, and surfacing which source
is currently in effect on the Service page. Making the CR authoritative instead
is the larger alternative.

## `task kind:up` does not restart application workloads

**Standard:** Convergence over one-shot interactions; treat the project as a
product.

`kind:up` rebuilds the api, ui, console-api and worker images and loads them
into the cluster, but only restarts the operator (`Taskfile.yaml:116`,
`.tasks/kind.yaml`). The workload images are tagged `:dev` with
`imagePullPolicy: IfNotPresent`, so a rebuild does not change any Deployment
spec and Kubernetes has no reason to roll the pods. The only `rollout restart`
in the tree is operator-scoped (`.tasks/lib/operator_k8s.sh:52`).

The result is that a local code change silently does not reach a running Kind
cluster. `kind:up` reports success while the pods keep serving the previous
build, so deployed checks and manual testing can both pass against stale code.
Application pods only pick up changes when something else happens to mutate
their spec.

**Why accepted:** not yet fixed; recorded so the failure mode is recognisable.
The workaround is an explicit restart after `kind:up`:

```bash
kubectl rollout restart deploy/tams-ui deploy/tams-api deploy/tams-console deploy/tams-worker -n tams
```

**Resolving it would take:** tagging workload images by content digest so the
Deployment spec changes when the code changes and Kubernetes rolls naturally.
Adding a `kubectl rollout restart` to `kind:up` is the smaller fix, but it is
imperative and restarts even when nothing changed.

## `service_description` does not follow the `TAMOSS_*` env convention

**Standard:** Reuse the same tooling, standards and formats; configuration flow.

Every setting in `Settings` binds to an explicit `TAMOSS_*` environment variable
through `validation_alias`, except `service_description`
(`src/app/tamoss/settings.py:173`), which has no alias and therefore binds to the
bare `SERVICE_DESCRIPTION`. The name was given a `TAMOSS_SERVICE_NAME` alias when
`.spec.displayName` was added; the description was left alone to keep that change
surgical.

**Why accepted:** pre-existing, cosmetic, and no deployment path sets the
variable today.

**Resolving it would take:** adding
`validation_alias="TAMOSS_SERVICE_DESCRIPTION"` in `src/app/tamoss/settings.py`
and documenting it alongside `TAMOSS_SERVICE_NAME` in
`docs/reference/runtime-configuration.md`. Anyone currently relying on
`SERVICE_DESCRIPTION` would need to rename the variable.

## The TAMS client keeps write methods the console cannot reach

**Standard:** Nothing speculative; treat the project as a product.

Fourteen `TamossApiClient` methods have no caller in `src/`. `deleteFlow`,
`getFlowTags`, `updateFlowTag`, `setFlowCollection`, `deleteFlowSegments`,
`createFlow`, `addFlowSegments`, `deleteObjectInstance`, `getWebhook`,
`createWebhook`, `getDeletionRequest`, `allocateStorage`,
`allocateStorageByCount` and `uploadRaw` are exercised only by
`tests/api/client.test.ts`. In production the `/api/` proxy permits `GET`,
`HEAD` and `OPTIONS` only (`src/app/frontend/ARCHITECTURE.md`), so no browser
path can reach the mutating ones at all.

**Why accepted:** these are the client half of the TAMS write surface, held for
the end-user-scoped mutation path that `ARCHITECTURE.md` names as future work.
They are deliberate and covered, not stranded — unlike the console-only dead
code removed alongside this entry. Recorded so the next dead-code sweep does not
re-open the question.

**Resolving it would take:** either landing the end-user-scoped write path so the
methods gain real callers, or deleting them with their tests and restoring them
from history when that path arrives.

## Dates render in whatever locale the operator's browser reports

**Standard:** Single source of truth; operator perspective first.

`utils/format.ts` has two locale-dependent formatters. `formatDate` calls
`toLocaleString()` with no locale and no options, so the rendered date depends on
the machine viewing it — the same record reads `11/08/2026` here and as 8 November
to a US-locale browser. `formatRelativeTime` passes `undefined` as the locale to
`Intl.RelativeTimeFormat`, which resolves the same way.

This is a time-addressable media store, so timestamps are the product. Two
operators reading the same screen during an incident and disagreeing about the
date is a poor property for the console to have.

**Why accepted:** not yet fixed. It has no effect on stored or API data — the
ambiguity is display-only — and it has not yet bitten anyone because the console
has so far been driven by one operator at a time.

**Resolving it would take:** picking an explicit format in `formatDate` — either
ISO-8601, or a pinned locale plus an explicit timezone — and rendering the
timezone alongside so a reader knows what they are looking at. Making the
timezone operator-configurable is the larger version.

## Raw TAMS timestamp pairs appear in labelled summary fields

**Standard:** End user experience for simplicity.

`utils/tams-time.ts` converts timestamps and timeranges into the TAMS wire form
(`timestampFromNanoseconds`, `timerangeFromNanoseconds`) but has no display-side
inverse, so any summary field rendering a timerange shows the raw
`seconds:nanoseconds` pair — a flow timerange reads `-1:600000000`. In a
labelled summary field that reads as a ratio or a clock time rather than a
duration.

**Why accepted:** this is a judgement call rather than a defect. The console
deliberately shows API truth, and the Raw Payload panels exist for exactly that.
The argument for changing it is that a labelled summary field is a different
context from a payload dump.

**Narrowed by the 8.2 console refresh:** the Service page instance is gone — it
now renders name, type, API version, service version and description only, so
the `300:0` and `30:0` timeout fields no longer appear. The remaining instances
are on the flow and source detail pages and have not been re-observed in a
browser since the refresh.

**Resolving it would take:** a display-side inverse of
`timestampFromNanoseconds` in `utils/tams-time.ts`, used in summary fields only,
leaving the Raw Payload panels untouched so both readings stay available.

## The refreshed console has not been observed at data scale

**Standard:** Short, provable iterative loops.

The 8.2 console has been read end to end in source and exercised against
`local-kind`, but the seeded instance holds one source and one flow, and several
of its routes are new. Unverified in a browser:

- **List density and pagination.** `useCursorPage` drives the catalog routes and
  the shared `CatalogToolbar`. Infinite scroll, cursor paging and table
  behaviour under load are untested; a few hundred records would be needed.
- **Toolbar and table widths.** The pre-refresh UI clipped table content while
  leaving column width unused, and broke toolbar button labels across three
  lines. `CatalogToolbar.tsx` and `Surface.module.css` are the shared primitives
  whose absence caused that, so the structural fix is in place — but the
  rendered result has not been checked at a desktop viewport.
- **New routes.** `/profiles`, `/profiles/:id`, `/deletions`, `/system`,
  `/sources/:id` and `/ingest/:runName` did not exist when this file was last
  walked. Only their source has been reviewed.
- **Objects list.** The API returned an empty collection, so `/objects` and
  object detail have only been seen in their empty state.
- **Ingest.** `/ingest` now lists durable `IngestRun` resources and offers
  cancellation only; create and retry are deliberately absent until the TAMSin
  contract is complete (`src/app/frontend/ARCHITECTURE.md`). No run has been
  driven from the UI through to registered segments.

**Why accepted:** the fixtures needed to exercise these are larger than the
current deployed checks, and the underlying API paths are covered by the
conformance and integration suites — it is the UI's behaviour over them that is
unverified.

**Resolving it would take:** a seed fixture with enough sources, flows, profiles
and objects to make list behaviour observable, and a deployed check that drives
an `IngestRun` to completion and asserts the resulting segments appear in the
UI.
